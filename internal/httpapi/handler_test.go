package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantmindSSI/haechi/internal/signing"
	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
	"github.com/QuantmindSSI/haechi/services/entitlement"
)

func newTestHandler(t *testing.T) (*controlapi.Service, http.Handler) {
	t.Helper()
	signer, err := signing.Generate()
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	verifier := entitlement.NewStaticVerifier()
	verifier.Add("admin-token", entitlement.Identity{
		Subject:      "admin-user",
		TenantID:     "platform",
		Entitlements: []string{ControlAdminEntitlement},
	})
	verifier.Add("user-token", entitlement.Identity{Subject: "regular-user", TenantID: "tenant-a"})
	svc := controlapi.NewService(controlapi.NewMemoryStore(), nil, signer, "test-key")
	return svc, New(svc, verifier)
}

func TestHealthIsPublicAndHardened(t *testing.T) {
	_, handler := newTestHandler(t)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestAdministrativeRoutesRequireAuthorizedBearer(t *testing.T) {
	_, handler := newTestHandler(t)
	body := []byte(`{"name":"Acme","idp_issuer":"https://idp.example"}`)

	unauthenticated := jsonRequest(http.MethodPost, "/api/v1/tenants", body)
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticatedRecorder.Code, http.StatusUnauthorized)
	}

	forbidden := jsonRequest(http.MethodPost, "/api/v1/tenants", body)
	forbidden.Header.Set("Authorization", "Bearer user-token")
	forbiddenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRecorder, forbidden)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d, want %d", forbiddenRecorder.Code, http.StatusForbidden)
	}

	authorized := jsonRequest(http.MethodPost, "/api/v1/tenants", body)
	authorized.Header.Set("Authorization", "Bearer admin-token")
	authorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusCreated {
		t.Fatalf("authorized status = %d, want %d; body=%s", authorizedRecorder.Code, http.StatusCreated, authorizedRecorder.Body.String())
	}
}

func TestStrictJSONDecoding(t *testing.T) {
	_, handler := newTestHandler(t)
	tests := []struct {
		name        string
		body        string
		contentType string
		want        int
	}{
		{name: "unknown field", body: `{"name":"Acme","unexpected":true}`, contentType: "application/json", want: http.StatusBadRequest},
		{name: "trailing document", body: `{"name":"Acme"}{"name":"Other"}`, contentType: "application/json", want: http.StatusBadRequest},
		{name: "wrong content type", body: `{"name":"Acme"}`, contentType: "text/plain", want: http.StatusUnsupportedMediaType},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewBufferString(test.body))
			req.Header.Set("Content-Type", test.contentType)
			req.Header.Set("Authorization", "Bearer admin-token")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestEnrollmentUsesProofInsteadOfAdministratorBearer(t *testing.T) {
	svc, handler := newTestHandler(t)
	tenant, err := svc.CreateTenant("Acme", "")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	code, err := svc.IssueEnrollmentCode(tenant.ID)
	if err != nil {
		t.Fatalf("issue enrollment code: %v", err)
	}
	deviceKey, err := signing.Generate()
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	proof, err := deviceKey.Sign(
		signing.ContextEnrollmentChallenge,
		controlapi.EnrollmentProofMessage(deviceKey.Public(), code.Challenge),
	)
	if err != nil {
		t.Fatalf("sign proof: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"code": code.Code, "public_key": deviceKey.Public(), "platform": "linux", "proof": proof,
	})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, jsonRequest(http.MethodPost, "/api/v1/enroll", body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
}

func TestPolicyRequestUsesOpenAPIFieldNames(t *testing.T) {
	svc, handler := newTestHandler(t)
	tenant, err := svc.CreateTenant("Acme", "")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	body := []byte(`{
		"allowed_service_classes":["general"],
		"allowed_transports":["direct"],
		"pq_requirement":"required",
		"kill_switch":true,
		"dns":["10.0.0.53"]
	}`)
	req := jsonRequest(http.MethodPut, "/api/v1/tenants/"+tenant.ID+"/policy", body)
	req.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	bundle, err := svc.PolicyBundle(tenant.ID)
	if err != nil {
		t.Fatalf("policy bundle: %v", err)
	}
	if bundle.Bundle.PQRequirement != "required" || len(bundle.Bundle.AllowedServiceClasses) != 1 {
		t.Fatalf("unexpected policy bundle: %+v", bundle.Bundle)
	}
}

func jsonRequest(method, target string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
