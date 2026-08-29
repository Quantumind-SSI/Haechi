// Package httpapi exposes the control-api Service over HTTP per
// api/openapi/control-api.yaml. It is a thin transport layer: all decisions live
// in the Service and its ports. Uses Go 1.22+ method+path routing.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
	"github.com/QuantmindSSI/haechi/services/entitlement"
)

// New returns an authenticated HTTP handler wired to the control-api service.
// Health and proof-of-possession enrollment are public; every administrative
// route requires a verified identity with ControlAdminEntitlement.
func New(svc *controlapi.Service, verifier entitlement.IdentityVerifier) http.Handler {
	mux := http.NewServeMux()
	admin := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, requireControlAdmin(verifier, handler))
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	admin("POST /api/v1/tenants", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name      string `json:"name"`
			IdPIssuer string `json:"idp_issuer"`
		}
		if !decode(w, r, &req) {
			return
		}
		t, err := svc.CreateTenant(req.Name, req.IdPIssuer)
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, http.StatusCreated, t)
	})

	admin("PUT /api/v1/tenants/{id}/policy", func(w http.ResponseWriter, r *http.Request) {
		var p controlapi.TenantPolicy
		if !decode(w, r, &p) {
			return
		}
		if err := svc.SetTenantPolicy(r.PathValue("id"), p); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	})

	admin("POST /api/v1/tenants/{id}/enrollment-codes", func(w http.ResponseWriter, r *http.Request) {
		code, err := svc.IssueEnrollmentCode(r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"code":       code.Code,
			"challenge":  code.Challenge,
			"expires_at": code.ExpiresAt.Format(time.RFC3339),
		})
	})

	mux.HandleFunc("POST /api/v1/enroll", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code      string            `json:"code"`
			PublicKey signing.PublicKey `json:"public_key"`
			Platform  string            `json:"platform"`
			Posture   string            `json:"posture"`
			Proof     []byte            `json:"proof"`
		}
		if !decode(w, r, &body) {
			return
		}
		dev, err := svc.EnrollDevice(controlapi.EnrollRequest{
			Code: body.Code, PublicKey: body.PublicKey, Platform: body.Platform,
			Posture: body.Posture, Proof: body.Proof,
		})
		if err != nil {
			writeErr(w, http.StatusForbidden, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"device_id": dev.ID, "tenant_id": dev.TenantID})
	})

	admin("DELETE /api/v1/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := svc.RevokeDevice(r.PathValue("id")); err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	admin("PUT /api/v1/gateways", func(w http.ResponseWriter, r *http.Request) {
		var g model.Gateway
		if !decode(w, r, &g) {
			return
		}
		if err := svc.RegisterGateway(g); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "upserted"})
	})

	admin("POST /api/v1/gateway-health", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			GatewayID string  `json:"gateway_id"`
			Healthy   bool    `json:"healthy"`
			Load      float64 `json:"load"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := svc.PublishGatewayHealth(body.GatewayID, body.Healthy, body.Load); err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Agent-facing: signed, rollback-protected policy bundle (§7 FetchPolicyBundle).
	admin("GET /api/v1/policy-bundle", func(w http.ResponseWriter, r *http.Request) {
		tenant := r.URL.Query().Get("tenant")
		bundle, err := svc.PolicyBundle(tenant)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, bundle)
	})

	return securityHeaders(mux)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content type must be application/json"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "response encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	payload = append(payload, '\n')
	if _, err := w.Write(payload); err != nil {
		return
	}
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
