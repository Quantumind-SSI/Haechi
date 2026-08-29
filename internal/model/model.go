// Package model holds the shared Haechi domain vocabulary used across the
// control plane, data plane and endpoint agent: tenants, devices, gateways and
// the two signed, rollback-protected artefacts (route plans and policy
// bundles) that cross trust boundaries.
//
// Signed artefacts carry their own Sign/Verify helpers built on
// internal/signing so producers and consumers agree on canonical bytes and
// domain separation.
package model

import (
	"encoding/json"
	"time"

	"github.com/QuantmindSSI/haechi/internal/signing"
)

// PQMode expresses the post-quantum reinforcement requirement for a route
// (ADR-005). "required" fails closed if Rosenpass negotiation fails.
type PQMode string

const (
	PQOff       PQMode = "off"
	PQPreferred PQMode = "preferred"
	PQRequired  PQMode = "required"
)

// Transport names an endpoint transport adapter (ADR-006).
type Transport string

const (
	TransportDirect    Transport = "direct"
	TransportProtected Transport = "protected"
)

// Tenant is an enterprise customer boundary.
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IdPIssuer string    `json:"idp_issuer"` // federated OIDC issuer (Keycloak-fronted)
	CreatedAt time.Time `json:"created_at"`
}

// Device is an enrolled endpoint. The device signing key proves possession on
// enrolment and authenticates later agent calls. It is never a user identifier.
type Device struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenant_id"`
	PublicKey  signing.PublicKey `json:"public_key"`
	Platform   string            `json:"platform"` // linux|macos|windows|android|ios
	Posture    string            `json:"posture"`  // coarse posture label
	EnrolledAt time.Time         `json:"enrolled_at"`
	Revoked    bool              `json:"revoked"`
	RevokedAt  *time.Time        `json:"revoked_at,omitempty"`
}

// Gateway is a data-plane egress node.
type Gateway struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	Region             string    `json:"region"`
	Audience           string    `json:"audience"` // capability audience/class served
	ServiceClasses     []string  `json:"service_classes"`
	Endpoint           string    `json:"endpoint"` // host:port
	WireGuardPublicKey string    `json:"wireguard_public_key"`
	PQCapable          bool      `json:"pq_capable"`
	Healthy            bool      `json:"healthy"`
	Load               float64   `json:"load"` // 0..1 coarse utilisation
	LastSeen           time.Time `json:"last_seen"`
}

// -------------------------------------------------------------------------
// Route plan (produced by route-orchestrator, verified by the agent).
// -------------------------------------------------------------------------

// RouteGateway is one eligible gateway in a route plan.
type RouteGateway struct {
	GatewayID          string  `json:"gateway_id"`
	Region             string  `json:"region"`
	Endpoint           string  `json:"endpoint"`
	WireGuardPublicKey string  `json:"wireguard_public_key"`
	Priority           int     `json:"priority"` // lower = preferred
	Score              float64 `json:"score"`
}

// RoutePlan is the unsigned core of a route resolution. It carries NO durable
// user identity (§7 ResolveRoute).
type RoutePlan struct {
	PlanID        string         `json:"plan_id"`
	TenantID      string         `json:"tenant_id"`
	Audience      string         `json:"audience"`
	ServiceClass  string         `json:"service_class"`
	Epoch         uint64         `json:"epoch"`
	PQRequirement PQMode         `json:"pq_requirement"`
	Gateways      []RouteGateway `json:"gateways"`
	IssuedAt      time.Time      `json:"issued_at"`
	ExpiresAt     time.Time      `json:"expires_at"`
}

func routeEnvelopeSigningBytes(signerKeyID string, plan RoutePlan) []byte {
	b, _ := json.Marshal(struct {
		SignerKeyID string    `json:"signer_key_id"`
		Plan        RoutePlan `json:"plan"`
	}{SignerKeyID: signerKeyID, Plan: plan})
	return b
}

// Expired reports whether the plan is outside its validity window at now.
func (p RoutePlan) Expired(now time.Time) bool {
	return now.Before(p.IssuedAt.Add(-30*time.Second)) || now.After(p.ExpiresAt)
}

// SignedRoutePlan wraps a plan with the orchestrator signature.
type SignedRoutePlan struct {
	Plan        RoutePlan `json:"plan"`
	SignerKeyID string    `json:"signer_key_id"`
	Signature   []byte    `json:"signature"`
}

// SignRoutePlan signs a route plan for the ResolveRoute contract using any
// Signer adapter (in-process key or OpenBao Transit).
func SignRoutePlan(signer signing.Signer, signerKeyID string, plan RoutePlan) (SignedRoutePlan, error) {
	sig, err := signer.Sign(signing.ContextRoutePlan, routeEnvelopeSigningBytes(signerKeyID, plan))
	if err != nil {
		return SignedRoutePlan{}, err
	}
	return SignedRoutePlan{Plan: plan, SignerKeyID: signerKeyID, Signature: sig}, nil
}

// Verify checks the orchestrator signature over the plan.
func (s SignedRoutePlan) Verify(pub signing.PublicKey) bool {
	return pub.Verify(signing.ContextRoutePlan, routeEnvelopeSigningBytes(s.SignerKeyID, s.Plan), s.Signature)
}

// -------------------------------------------------------------------------
// Policy bundle (produced by control-api, verified by the agent).
// -------------------------------------------------------------------------

// PolicyBundle is the unsigned core of endpoint policy (§7 FetchPolicyBundle).
// Version is monotonic per tenant and used for rollback protection.
type PolicyBundle struct {
	BundleID              string      `json:"bundle_id"`
	TenantID              string      `json:"tenant_id"`
	Version               uint64      `json:"version"`
	Epoch                 uint64      `json:"epoch"`
	KillSwitch            bool        `json:"kill_switch"`
	AllowedServiceClasses []string    `json:"allowed_service_classes"`
	AllowedTransports     []Transport `json:"allowed_transports"`
	PQRequirement         PQMode      `json:"pq_requirement"`
	DNS                   []string    `json:"dns"`
	IssuedAt              time.Time   `json:"issued_at"`
	ExpiresAt             time.Time   `json:"expires_at"`
}

func policyEnvelopeSigningBytes(signerKeyID string, bundle PolicyBundle) []byte {
	raw, _ := json.Marshal(struct {
		SignerKeyID string       `json:"signer_key_id"`
		Bundle      PolicyBundle `json:"bundle"`
	}{SignerKeyID: signerKeyID, Bundle: bundle})
	return raw
}

// SignedPolicyBundle wraps a bundle with the control-plane signature.
type SignedPolicyBundle struct {
	Bundle      PolicyBundle `json:"bundle"`
	SignerKeyID string       `json:"signer_key_id"`
	Signature   []byte       `json:"signature"`
}

// SignPolicyBundle signs a policy bundle for distribution using any Signer
// adapter (in-process key or OpenBao Transit).
func SignPolicyBundle(signer signing.Signer, signerKeyID string, bundle PolicyBundle) (SignedPolicyBundle, error) {
	sig, err := signer.Sign(signing.ContextPolicyBundle, policyEnvelopeSigningBytes(signerKeyID, bundle))
	if err != nil {
		return SignedPolicyBundle{}, err
	}
	return SignedPolicyBundle{Bundle: bundle, SignerKeyID: signerKeyID, Signature: sig}, nil
}

// Verify checks the control-plane signature over the bundle.
func (s SignedPolicyBundle) Verify(pub signing.PublicKey) bool {
	return pub.Verify(signing.ContextPolicyBundle, policyEnvelopeSigningBytes(s.SignerKeyID, s.Bundle), s.Signature)
}
