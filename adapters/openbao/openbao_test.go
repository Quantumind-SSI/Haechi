package openbao

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantmindSSI/haechi/internal/signing"
)

// fakeTransit emulates OpenBao Transit for an ed25519 key, so we exercise the
// real wire contract and prove signatures verify with internal/signing.
func fakeTransit(t *testing.T) (*httptest.Server, ed25519.PublicKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/transit/keys/haechi-control", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"latest_version": 1,
				"keys": map[string]any{
					"1": map[string]any{"public_key": base64.StdEncoding.EncodeToString(pub)},
				},
			},
		})
	})
	mux.HandleFunc("/v1/transit/sign/haechi-control", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Input      string `json:"input"`
			KeyVersion int    `json:"key_version"`
		}
		_ = json.Unmarshal(body, &in)
		if in.KeyVersion != 1 {
			http.Error(w, "unexpected key version", http.StatusBadRequest)
			return
		}
		raw, _ := base64.StdEncoding.DecodeString(in.Input)
		sig := ed25519.Sign(priv, raw)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"signature": "vault:v1:" + base64.StdEncoding.EncodeToString(sig)},
		})
	})
	return httptest.NewServer(mux), pub
}

func TestOpenBaoSignVerifies(t *testing.T) {
	srv, pub := fakeTransit(t)
	defer srv.Close()

	s, err := New(context.Background(), Config{
		Address: srv.URL, Token: "dev-token", KeyName: "haechi-control", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	// Public key round-trips.
	want, _ := signing.PublicKeyFromBytes(pub)
	if !s.Public().Equal(want) {
		t.Fatal("fetched public key mismatch")
	}
	if s.KeyID() != "openbao:haechi-control:v1" {
		t.Fatalf("key id = %q", s.KeyID())
	}

	// A signature produced via Transit verifies with the domain-separated verifier.
	msg := []byte("route-plan-bytes")
	sig, err := s.Sign(signing.ContextRoutePlan, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !s.Public().Verify(signing.ContextRoutePlan, msg, sig) {
		t.Fatal("OpenBao-produced signature failed verification")
	}
	// Cross-context must fail (domain separation preserved through Transit).
	if s.Public().Verify(signing.ContextPolicyBundle, msg, sig) {
		t.Fatal("cross-context signature verified")
	}
}

func TestOpenBaoSelectsHighestAvailableKeyDeterministically(t *testing.T) {
	pub1, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key 1: %v", err)
	}
	pub2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key 2: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"latest_version": 3,
				"keys": map[string]any{
					"1": map[string]any{"public_key": base64.StdEncoding.EncodeToString(pub1)},
					"2": map[string]any{"public_key": base64.StdEncoding.EncodeToString(pub2)},
				},
			},
		})
	}))
	defer srv.Close()

	signer, err := New(context.Background(), Config{
		Address: srv.URL, KeyName: "haechi-control", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	want, err := signing.PublicKeyFromBytes(pub2)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	if signer.KeyID() != "openbao:haechi-control:v2" || !signer.Public().Equal(want) {
		t.Fatalf("selected key = %q", signer.KeyID())
	}
}
