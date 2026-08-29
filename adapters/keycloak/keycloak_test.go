package keycloak

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// signJWT crafts an RS256 JWT for tests, mirroring what Keycloak emits.
func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	seg := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	digest := sha256.Sum256([]byte(seg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return seg + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// oidcServer stands up discovery + JWKS endpoints backed by key.
func oidcServer(t *testing.T, key *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   base,
			"jwks_uri": base + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{"kty": "RSA", "kid": kid, "use": "sig", "n": n, "e": e}},
		})
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	return srv
}

func TestKeycloakVerifyValidToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := oidcServer(t, key, "kid-1")
	defer srv.Close()

	v := New(Config{Issuer: srv.URL, Audience: "haechi", HTTPClient: srv.Client()})
	token := signJWT(t, key, "kid-1", map[string]any{
		"iss":                 srv.URL,
		"sub":                 "user-123",
		"aud":                 "haechi",
		"exp":                 time.Now().Add(time.Hour).Unix(),
		"tenant":              "ten_abc",
		"haechi_entitlements": []string{"eu-standard|general"},
	})

	id, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Subject != "user-123" || id.TenantID != "ten_abc" {
		t.Fatalf("bad identity: %+v", id)
	}
	if !id.Entitled("eu-standard", "general") {
		t.Fatal("entitlement not extracted")
	}
	if id.Entitled("us-standard", "general") {
		t.Fatal("unexpected entitlement")
	}
}

func TestKeycloakRejectsBadSignature(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := oidcServer(t, key, "kid-1")
	defer srv.Close()

	v := New(Config{Issuer: srv.URL, Audience: "haechi", HTTPClient: srv.Client()})
	// Signed with a different key than the JWKS advertises.
	token := signJWT(t, other, "kid-1", map[string]any{
		"iss": srv.URL, "sub": "user", "aud": "haechi", "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("bad signature accepted")
	}
}

func TestKeycloakRejectsExpired(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := oidcServer(t, key, "kid-1")
	defer srv.Close()

	v := New(Config{Issuer: srv.URL, Audience: "haechi", HTTPClient: srv.Client()})
	token := signJWT(t, key, "kid-1", map[string]any{
		"iss": srv.URL, "sub": "user", "aud": "haechi", "exp": time.Now().Add(-time.Minute).Unix(),
	})
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestKeycloakRejectsWrongAudience(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := oidcServer(t, key, "kid-1")
	defer srv.Close()

	v := New(Config{Issuer: srv.URL, Audience: "haechi", HTTPClient: srv.Client()})
	token := signJWT(t, key, "kid-1", map[string]any{
		"iss": srv.URL, "sub": "user", "aud": "some-other-client", "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("wrong audience accepted")
	}
}

func TestKeycloakRejectsMissingExpiry(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	srv := oidcServer(t, key, "kid-1")
	defer srv.Close()

	v := New(Config{Issuer: srv.URL, Audience: "haechi", HTTPClient: srv.Client()})
	token := signJWT(t, key, "kid-1", map[string]any{
		"iss": srv.URL, "sub": "user", "aud": "haechi",
	})
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("token without expiry was accepted")
	}
}

func TestKeycloakRejectsMissingAudienceConfiguration(t *testing.T) {
	v := New(Config{Issuer: "https://identity.example"})
	if _, err := v.Verify(context.Background(), "not-a-token"); err != ErrConfig {
		t.Fatalf("want ErrConfig, got %v", err)
	}
}

func TestKeycloakRejectsDiscoveryIssuerMismatch(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": "https://attacker.example", "jwks_uri": "https://attacker.example/jwks",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	v := New(Config{Issuer: srv.URL, Audience: "haechi", HTTPClient: srv.Client()})
	token := signJWT(t, key, "kid-1", map[string]any{
		"iss": srv.URL, "sub": "user", "aud": "haechi", "exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Verify(context.Background(), token); err == nil {
		t.Fatal("mismatched discovery issuer was accepted")
	}
}
