// Package openbao is the production adapter for the signing.Signer port, wiring
// OpenBao's Transit secrets engine (third_party/manifest.yaml: openbao 2.0.1).
// Private signing keys live in OpenBao and never in an application pod (§13 key
// management). It is the ONLY place that talks to OpenBao.
//
// It signs Ed25519 over the exact domain-separated bytes produced by
// internal/signing, so signatures verify identically whether produced here or by
// the in-process dev key.
//
//	GET  {base}/v1/transit/keys/{key}     -> data.keys[latest].public_key (base64)
//	POST {base}/v1/transit/sign/{key}     {"input": b64(framed)} -> data.signature "vault:vN:b64"
package openbao

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantmindSSI/haechi/internal/signing"
)

// Config configures the Transit signer.
type Config struct {
	Address    string // e.g. https://openbao:8200
	Token      string // workload identity token
	KeyName    string // transit key name (ed25519)
	Mount      string // transit mount path (default "transit")
	HTTPClient *http.Client
}

// Signer implements signing.Signer against OpenBao Transit.
type Signer struct {
	cfg     Config
	pub     signing.PublicKey
	mount   string
	version int
}

var _ signing.Signer = (*Signer)(nil)

// New builds the signer and fetches the current public key so Public() works
// offline thereafter.
func New(ctx context.Context, cfg Config) (*Signer, error) {
	if cfg.Mount == "" {
		cfg.Mount = "transit"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	s := &Signer{cfg: cfg, mount: strings.Trim(cfg.Mount, "/")}
	pub, version, err := s.fetchPublicKey(ctx)
	if err != nil {
		return nil, err
	}
	s.pub = pub
	s.version = version
	return s, nil
}

// Public returns the Ed25519 verification key.
func (s *Signer) Public() signing.PublicKey { return s.pub }

// KeyID identifies the exact Transit key version paired with Public().
func (s *Signer) KeyID() string {
	return fmt.Sprintf("openbao:%s:v%d", s.cfg.KeyName, s.version)
}

// Sign frames msg with the domain-separation context and asks Transit to sign
// the framed bytes with the Ed25519 key.
func (s *Signer) Sign(ctx signing.Context, msg []byte) ([]byte, error) {
	framed, err := signing.Frame(ctx, msg)
	if err != nil {
		return nil, err
	}
	reqBody, err := json.Marshal(map[string]any{
		"input":       base64.StdEncoding.EncodeToString(framed),
		"key_version": s.version,
		"prehashed":   false,
	})
	if err != nil {
		return nil, fmt.Errorf("openbao: encode sign request: %w", err)
	}
	url := fmt.Sprintf("%s/v1/%s/sign/%s", strings.TrimRight(s.cfg.Address, "/"), s.mount, s.cfg.KeyName)
	var out struct {
		Data struct {
			Signature string `json:"signature"`
		} `json:"data"`
	}
	if err := s.do(context.Background(), http.MethodPost, url, reqBody, &out); err != nil {
		return nil, err
	}
	return decodeVaultSignature(out.Data.Signature)
}

func (s *Signer) fetchPublicKey(ctx context.Context) (signing.PublicKey, int, error) {
	url := fmt.Sprintf("%s/v1/%s/keys/%s", strings.TrimRight(s.cfg.Address, "/"), s.mount, s.cfg.KeyName)
	var out struct {
		Data struct {
			LatestVersion int `json:"latest_version"`
			Keys          map[string]struct {
				PublicKey string `json:"public_key"`
			} `json:"keys"`
		} `json:"data"`
	}
	if err := s.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return signing.PublicKey{}, 0, err
	}
	version := out.Data.LatestVersion
	entry, ok := out.Data.Keys[strconv.Itoa(version)]
	if !ok {
		version = 0
		for rawVersion := range out.Data.Keys {
			candidate, err := strconv.Atoi(rawVersion)
			if err == nil && candidate > version {
				version = candidate
			}
		}
		entry, ok = out.Data.Keys[strconv.Itoa(version)]
	}
	if !ok || version < 1 || entry.PublicKey == "" {
		return signing.PublicKey{}, 0, errors.New("openbao: no public key in transit key response")
	}
	raw, err := base64.StdEncoding.DecodeString(entry.PublicKey)
	if err != nil {
		return signing.PublicKey{}, 0, fmt.Errorf("openbao: decode public key: %w", err)
	}
	pub, err := signing.PublicKeyFromBytes(raw)
	return pub, version, err
}

func (s *Signer) do(ctx context.Context, method, url string, body []byte, out any) (err error) {
	var r *http.Request
	if body != nil {
		r, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	} else {
		r, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return err
	}
	if s.cfg.Token != "" {
		r.Header.Set("X-Vault-Token", s.cfg.Token)
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := s.cfg.HTTPClient.Do(r)
	if err != nil {
		return fmt.Errorf("openbao: request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("openbao: close response: %w", closeErr))
		}
	}()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("openbao: status %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("openbao: response contains trailing JSON")
	}
	return nil
}

// decodeVaultSignature strips the "vault:vN:" prefix and base64-decodes the sig.
func decodeVaultSignature(s string) ([]byte, error) {
	i := strings.LastIndex(s, ":")
	if i < 0 || !strings.HasPrefix(s, "vault:") {
		return nil, fmt.Errorf("openbao: malformed signature %q", s)
	}
	raw, err := base64.StdEncoding.DecodeString(s[i+1:])
	if err != nil {
		return nil, fmt.Errorf("openbao: decode signature: %w", err)
	}
	return raw, nil
}
