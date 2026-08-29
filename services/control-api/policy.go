package controlapi

import "context"

// PolicyEngine is the authorisation PORT (Integration Document §5 OPA row). The
// production adapter (adapters/opa) sends AuthzInput to OPA's REST API and reads
// back a decision; StaticEngine is the dev adapter with identical decision
// shape. Deny-by-default and fail-closed-on-error are contractual for every
// implementation.
type PolicyEngine interface {
	Authorize(ctx context.Context, in AuthzInput) (Decision, error)
}

// AuthzInput is the document evaluated by the policy engine. It mirrors the
// `input` document sent to OPA (`data.haechi.authz.decision`).
type AuthzInput struct {
	DeviceID              string   `json:"device_id"`
	DeviceRevoked         bool     `json:"device_revoked"`
	TenantID              string   `json:"tenant_id"`
	Platform              string   `json:"platform"`
	Posture               string   `json:"posture"`
	ServiceClass          string   `json:"service_class"`
	AllowedServiceClasses []string `json:"allowed_service_classes"`
}

// StaticEngine is the deny-by-default dev adapter. It encodes the same rule the
// bundled Rego policy encodes (deploy/opa/policy/authz.rego).
type StaticEngine struct{}

// Authorize implements PolicyEngine.
func (StaticEngine) Authorize(_ context.Context, in AuthzInput) (Decision, error) {
	if in.DeviceRevoked {
		return Decision{false, "device revoked"}, nil
	}
	if !contains(in.AllowedServiceClasses, in.ServiceClass) {
		return Decision{false, "service class not permitted"}, nil
	}
	return Decision{true, "ok"}, nil
}
