package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/QuantmindSSI/haechi/services/entitlement"
)

// ControlAdminEntitlement grants access to the administrative control API.
const ControlAdminEntitlement = "haechi:control-admin"

type principalContextKey struct{}

func requireControlAdmin(verifier entitlement.IdentityVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok || verifier == nil {
			writeUnauthorized(w)
			return
		}
		identity, err := verifier.Verify(r.Context(), token)
		if err != nil {
			writeUnauthorized(w)
			return
		}
		if !hasEntitlement(identity.Entitlements, ControlAdminEntitlement) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions"})
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

func hasEntitlement(entitlements []string, required string) bool {
	for _, entitlement := range entitlements {
		if entitlement == required {
			return true
		}
	}
	return false
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="haechi-control"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
