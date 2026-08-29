// Package keycloak is the production adapter for the entitlement IdentityVerifier
// port, wiring Keycloak (third_party/manifest.yaml: keycloak 25.0.6) via standard
// OIDC: discovery -> JWKS -> RS256 JWT verification. It is the ONLY place that
// talks to the identity provider, and the verified token NEVER leaves this plane
// (the gateway never receives it, §4.1).
//
// Implemented with the standard library only (net/http, crypto/rsa) to keep the
// identity-verification surface small and auditable.
package keycloak

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantmindSSI/haechi/services/entitlement"
)

// Errors.
var (
	ErrConfig = errors.New("keycloak: invalid verifier configuration")
	ErrToken  = errors.New("keycloak: invalid token")
)

// Config configures the OIDC verifier.
type Config struct {
	Issuer            string // e.g. https://keycloak/realms/haechi
	Audience          string // expected `aud` (Haechi client id)
	TenantClaim       string // claim holding tenant id (default "tenant")
	EntitlementsClaim string // claim holding []string (default "haechi_entitlements")
	HTTPClient        *http.Client
	JWKSTTL           time.Duration
}

// Verifier validates Keycloak-issued OIDC tokens. It satisfies
// entitlement.IdentityVerifier.
type Verifier struct {
	cfg Config

	mu        sync.RWMutex
	jwksURI   string
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

var _ entitlement.IdentityVerifier = (*Verifier)(nil)

// New builds the verifier. Discovery is performed lazily on first Verify.
func New(cfg Config) *Verifier {
	if cfg.TenantClaim == "" {
		cfg.TenantClaim = "tenant"
	}
	if cfg.EntitlementsClaim == "" {
		cfg.EntitlementsClaim = "haechi_entitlements"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.JWKSTTL == 0 {
		cfg.JWKSTTL = 10 * time.Minute
	}
	return &Verifier{cfg: cfg, keys: map[string]*rsa.PublicKey{}}
}

// Verify authenticates a JWT and maps it to a Haechi identity.
func (v *Verifier) Verify(ctx context.Context, token string) (entitlement.Identity, error) {
	if v.cfg.Issuer == "" || v.cfg.Audience == "" {
		return entitlement.Identity{}, ErrConfig
	}
	header, claims, signingInput, sig, err := parseJWT(token)
	if err != nil {
		return entitlement.Identity{}, err
	}
	if header.Alg != "RS256" {
		return entitlement.Identity{}, fmt.Errorf("%w: unsupported alg %q", ErrToken, header.Alg)
	}

	key, err := v.keyForKID(ctx, header.Kid)
	if err != nil {
		return entitlement.Identity{}, err
	}
	digest := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return entitlement.Identity{}, fmt.Errorf("%w: signature", ErrToken)
	}

	// Standard claim validation.
	now := time.Now()
	if claims.Issuer != v.cfg.Issuer {
		return entitlement.Identity{}, fmt.Errorf("%w: issuer", ErrToken)
	}
	if claims.Exp == 0 {
		return entitlement.Identity{}, fmt.Errorf("%w: missing exp", ErrToken)
	}
	if now.After(time.Unix(claims.Exp, 0)) {
		return entitlement.Identity{}, fmt.Errorf("%w: expired", ErrToken)
	}
	if claims.Nbf != 0 && now.Before(time.Unix(claims.Nbf, 0)) {
		return entitlement.Identity{}, fmt.Errorf("%w: not yet valid", ErrToken)
	}
	if v.cfg.Audience != "" && !claims.hasAudience(v.cfg.Audience) {
		return entitlement.Identity{}, fmt.Errorf("%w: audience", ErrToken)
	}

	id := entitlement.Identity{
		Subject:      claims.Subject,
		TenantID:     claims.stringClaim(v.cfg.TenantClaim),
		Entitlements: claims.stringSlice(v.cfg.EntitlementsClaim),
	}
	if id.Subject == "" {
		return entitlement.Identity{}, fmt.Errorf("%w: missing sub", ErrToken)
	}
	return id, nil
}

// keyForKID returns the RSA key for a kid, refreshing JWKS if unknown or stale.
func (v *Verifier) keyForKID(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	k, ok := v.keys[kid]
	fresh := time.Since(v.fetchedAt) < v.cfg.JWKSTTL
	v.mu.RUnlock()
	if ok && fresh {
		return k, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w: unknown key id %q", ErrToken, kid)
}

// refresh performs OIDC discovery (once) and fetches the JWKS.
func (v *Verifier) refresh(ctx context.Context) error {
	v.mu.Lock()
	uri := v.jwksURI
	v.mu.Unlock()

	if uri == "" {
		disco := v.cfg.Issuer + "/.well-known/openid-configuration"
		var meta struct {
			JWKSURI string `json:"jwks_uri"`
			Issuer  string `json:"issuer"`
		}
		if err := v.getJSON(ctx, disco, &meta); err != nil {
			return fmt.Errorf("keycloak: discovery: %w", err)
		}
		if meta.JWKSURI == "" {
			return errors.New("keycloak: discovery missing jwks_uri")
		}
		if meta.Issuer != v.cfg.Issuer {
			return errors.New("keycloak: discovery issuer mismatch")
		}
		uri = meta.JWKSURI
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
			Use string `json:"use"`
		} `json:"keys"`
	}
	if err := v.getJSON(ctx, uri, &jwks); err != nil {
		return fmt.Errorf("keycloak: jwks: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(new(big.Int).SetBytes(eb).Int64())}
	}
	if len(keys) == 0 {
		return errors.New("keycloak: jwks contained no usable RSA keys")
	}
	v.mu.Lock()
	v.jwksURI = uri
	v.keys = keys
	v.fetchedAt = time.Now()
	v.mu.Unlock()
	return nil
}

func (v *Verifier) getJSON(ctx context.Context, url string, out any) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := v.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("keycloak: close response: %w", closeErr))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("response contains trailing JSON")
	}
	return nil
}

// --- JWT parsing (stdlib) ---

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtClaims struct {
	Issuer  string          `json:"iss"`
	Subject string          `json:"sub"`
	Exp     int64           `json:"exp"`
	Nbf     int64           `json:"nbf"`
	Aud     json.RawMessage `json:"aud"`
	raw     map[string]json.RawMessage
}

func (c jwtClaims) hasAudience(aud string) bool {
	// aud may be a string or an array of strings.
	var single string
	if err := json.Unmarshal(c.Aud, &single); err == nil {
		return single == aud
	}
	var many []string
	if err := json.Unmarshal(c.Aud, &many); err == nil {
		for _, a := range many {
			if a == aud {
				return true
			}
		}
	}
	return false
}

func (c jwtClaims) stringClaim(name string) string {
	raw, ok := c.raw[name]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func (c jwtClaims) stringSlice(name string) []string {
	raw, ok := c.raw[name]
	if !ok {
		return nil
	}
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

func parseJWT(token string) (jwtHeader, jwtClaims, string, []byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtHeader{}, jwtClaims{}, "", nil, fmt.Errorf("%w: not a JWT", ErrToken)
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return jwtHeader{}, jwtClaims{}, "", nil, fmt.Errorf("%w: header", ErrToken)
	}
	var h jwtHeader
	if err := json.Unmarshal(hb, &h); err != nil {
		return jwtHeader{}, jwtClaims{}, "", nil, fmt.Errorf("%w: header json", ErrToken)
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtHeader{}, jwtClaims{}, "", nil, fmt.Errorf("%w: payload", ErrToken)
	}
	var c jwtClaims
	if err := json.Unmarshal(pb, &c); err != nil {
		return jwtHeader{}, jwtClaims{}, "", nil, fmt.Errorf("%w: payload json", ErrToken)
	}
	_ = json.Unmarshal(pb, &c.raw)
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return jwtHeader{}, jwtClaims{}, "", nil, fmt.Errorf("%w: signature", ErrToken)
	}
	return h, c, parts[0] + "." + parts[1], sig, nil
}
