package opa

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
)

// fakeOPA mimics OPA's decision document so we test the wire contract without
// the real server. The Rego it stands in for is deploy/opa/policy/authz.rego.
func fakeOPA(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/data/haechi/authz/decision" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Input controlapi.AuthzInput `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		in := body.Input
		allow := !in.DeviceRevoked && contains(in.AllowedServiceClasses, in.ServiceClass)
		reason := "ok"
		if !allow {
			reason = "denied"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"allow": allow, "reason": reason},
		})
	}))
}

func TestOPAAllow(t *testing.T) {
	srv := fakeOPA(t)
	defer srv.Close()
	c := New(srv.URL)
	dec, err := c.Authorize(context.Background(), controlapi.AuthzInput{
		ServiceClass: "general", AllowedServiceClasses: []string{"general"},
	})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !dec.Allow {
		t.Fatalf("expected allow, got deny: %s", dec.Reason)
	}
}

func TestOPADenyRevoked(t *testing.T) {
	srv := fakeOPA(t)
	defer srv.Close()
	c := New(srv.URL)
	dec, _ := c.Authorize(context.Background(), controlapi.AuthzInput{
		DeviceRevoked: true, ServiceClass: "general", AllowedServiceClasses: []string{"general"},
	})
	if dec.Allow {
		t.Fatal("revoked device must be denied by OPA")
	}
}

func TestOPAFailClosedOnError(t *testing.T) {
	// Point at a closed port; the adapter must return an error so the service
	// fails closed.
	c := New("http://127.0.0.1:0")
	if _, err := c.Authorize(context.Background(), controlapi.AuthzInput{}); err == nil {
		t.Fatal("expected transport error (fail closed)")
	}
}

func TestOPAMissingResultDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{}) // no "result"
	}))
	defer srv.Close()
	c := New(srv.URL)
	dec, err := c.Authorize(context.Background(), controlapi.AuthzInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Allow {
		t.Fatal("missing result must deny by default")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
