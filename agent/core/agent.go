// Package agentcore is the Haechi endpoint agent core (Integration Document
// §6.1, §9). It is the only component that coordinates tunnel state on a device.
// It drives the full connection flow: policy, entitlement, unlinkable
// capability, signed route resolution, gateway admission and tunnel bring-up,
// with fail-closed semantics and an early kill switch.
//
// External services are reached through the Deps callbacks (in-process for the
// vertical slice, mutually-authenticated gRPC in production). The core holds no
// ambient OS privilege; tunnel changes go through the tunnel.Tunnel adapter.
package agentcore

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/QuantmindSSI/haechi/agent/tunnel"
	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
	"github.com/QuantmindSSI/haechi/pkg/capability"
	capissuer "github.com/QuantmindSSI/haechi/services/capability-issuer"
	"github.com/QuantmindSSI/haechi/services/entitlement"
)

const maxRouteGateways = 16

// Fail-closed errors. Every one leaves the kill switch engaged and no tunnel up.
var (
	ErrDependencies      = errors.New("agent: required dependency is not configured")
	ErrPolicySignature   = errors.New("agent: policy bundle signature invalid")
	ErrPolicyTenant      = errors.New("agent: policy bundle tenant mismatch")
	ErrPolicyExpired     = errors.New("agent: policy bundle outside validity window")
	ErrPolicyRollback    = errors.New("agent: policy bundle version rollback detected")
	ErrServiceClass      = errors.New("agent: service class not permitted by policy")
	ErrGrantContext      = errors.New("agent: entitlement grant context mismatch")
	ErrRouteSignature    = errors.New("agent: route plan signature invalid")
	ErrRouteContext      = errors.New("agent: route plan context mismatch")
	ErrRouteExpired      = errors.New("agent: route plan expired")
	ErrNoRoute           = errors.New("agent: no eligible gateway in route plan")
	ErrAllGatewaysFailed = errors.New("agent: all eligible gateways failed")
	ErrPQDowngrade       = errors.New("agent: required post-quantum reinforcement not negotiated")
	ErrTunnelState       = errors.New("agent: tunnel reported an invalid state")
	ErrTunnelCleanup     = errors.New("agent: tunnel cleanup failed")
)

// Trust holds the public keys the agent verifies signed artefacts against.
// Distributed out of band at enrolment (pinned).
type Trust struct {
	ControlPlanePub signing.PublicKey // policy bundles
	RoutePlanPub    signing.PublicKey // route plans
}

// AdmitFunc presents a capability to a gateway. Tunnel lifecycle remains under
// the agent's control so every failed bring-up can be torn down deterministically.
type AdmitFunc func(tok *capability.Token, gw model.RouteGateway) error

// Deps are the collaborators the agent calls out to. The composition root wires
// each to an in-process service (dev) or a gRPC client (prod); the agent core is
// unaware of which. Authentication tokens are bound in the composition root.
type Deps struct {
	FetchPolicy     func(tenantID string) (model.SignedPolicyBundle, error)
	Entitle         func(audience, serviceClass string) (entitlement.SignedGrant, error)
	IssuerPublicKey func(grant entitlement.SignedGrant, attrs capability.Attributes) (capability.KeyID, *rsa.PublicKey, error)
	IssueCapability func(req capissuer.IssueRequest) (capability.KeyID, []byte, error)
	ResolveRoute    func(tenantID, audience, serviceClass string, pq model.PQMode) (model.SignedRoutePlan, error)
	Admit           AdmitFunc
	Tunnel          tunnel.Tunnel
}

// Agent is an enrolled endpoint.
type Agent struct {
	DeviceID  string
	TenantID  string
	Subject   string
	deviceKey signing.PrivateKey
	trust     Trust
	deps      Deps

	lastPolicyVersion uint64
	killSwitch        bool
	now               func() time.Time
}

// New constructs an enrolled agent.
func New(deviceID, tenantID, subject string, deviceKey signing.PrivateKey, trust Trust, deps Deps) *Agent {
	return &Agent{
		DeviceID:   deviceID,
		TenantID:   tenantID,
		Subject:    subject,
		deviceKey:  deviceKey,
		trust:      trust,
		deps:       deps,
		killSwitch: true,
		now:        time.Now,
	}
}

// SetClock overrides the time source (tests).
func (a *Agent) SetClock(now func() time.Time) { a.now = now }

// KillSwitchEngaged reports whether the kill switch is currently engaged.
func (a *Agent) KillSwitchEngaged() bool { return a.killSwitch }

// Session is the result of a successful connection.
type Session struct {
	GatewayID    string
	ServiceClass string
	NegotiatedPQ model.PQMode
	KillSwitch   bool
}

// Connect runs the end-to-end flow (§9). On any failure it returns an error
// with the kill switch engaged and no tunnel up (fail closed).
func (a *Agent) Connect(audience, serviceClass string) (Session, error) {
	a.killSwitch = true
	if err := a.validateDependencies(); err != nil {
		return Session{}, err
	}
	if err := a.deps.Tunnel.Down(); err != nil {
		return Session{}, fmt.Errorf("%w: %v", ErrTunnelCleanup, err)
	}
	now := a.now()
	pol, err := a.loadPolicy(now, serviceClass)
	if err != nil {
		return Session{}, err
	}
	grant, err := a.deps.Entitle(audience, serviceClass)
	if err != nil {
		return Session{}, err
	}
	if !a.validGrant(grant, audience, serviceClass, now) {
		return Session{}, ErrGrantContext
	}
	attrs := capability.Attributes{Audience: audience, ServiceClass: serviceClass, Epoch: grant.Grant.Epoch}
	plan, err := a.loadRoute(now, pol, attrs)
	if err != nil {
		return Session{}, err
	}
	var lastErr error
	for _, gw := range plan.Gateways {
		session, connectErr := a.connectGateway(grant, attrs, pol, gw)
		if connectErr == nil {
			return session, nil
		}
		lastErr = connectErr
	}
	if lastErr == nil {
		lastErr = ErrAllGatewaysFailed
	}
	return Session{}, lastErr
}

func (a *Agent) validateDependencies() error {
	if a.deps.FetchPolicy == nil || a.deps.Entitle == nil || a.deps.IssuerPublicKey == nil ||
		a.deps.IssueCapability == nil || a.deps.ResolveRoute == nil || a.deps.Admit == nil || a.deps.Tunnel == nil {
		return ErrDependencies
	}
	return nil
}

func (a *Agent) loadPolicy(now time.Time, serviceClass string) (model.PolicyBundle, error) {
	signed, err := a.deps.FetchPolicy(a.TenantID)
	if err != nil {
		return model.PolicyBundle{}, err
	}
	if !signed.Verify(a.trust.ControlPlanePub) {
		return model.PolicyBundle{}, ErrPolicySignature
	}
	pol := signed.Bundle
	if pol.TenantID != a.TenantID {
		return model.PolicyBundle{}, ErrPolicyTenant
	}
	if pol.Version < a.lastPolicyVersion {
		return model.PolicyBundle{}, ErrPolicyRollback
	}
	if now.Before(pol.IssuedAt.Add(-30*time.Second)) || now.After(pol.ExpiresAt) {
		return model.PolicyBundle{}, ErrPolicyExpired
	}
	if !contains(pol.AllowedServiceClasses, serviceClass) {
		return model.PolicyBundle{}, ErrServiceClass
	}
	a.lastPolicyVersion = pol.Version
	return pol, nil
}

func (a *Agent) validGrant(grant entitlement.SignedGrant, audience, serviceClass string, now time.Time) bool {
	g := grant.Grant
	return g.TenantID == a.TenantID && g.Subject == a.Subject && g.Audience == audience &&
		g.ServiceClass == serviceClass && !now.Before(g.IssuedAt.Add(-30*time.Second)) && !now.After(g.ExpiresAt)
}

func (a *Agent) loadRoute(now time.Time, pol model.PolicyBundle, attrs capability.Attributes) (model.RoutePlan, error) {
	signed, err := a.deps.ResolveRoute(a.TenantID, attrs.Audience, attrs.ServiceClass, pol.PQRequirement)
	if err != nil {
		return model.RoutePlan{}, err
	}
	if !signed.Verify(a.trust.RoutePlanPub) {
		return model.RoutePlan{}, ErrRouteSignature
	}
	plan := signed.Plan
	if plan.TenantID != a.TenantID || plan.Audience != attrs.Audience ||
		plan.ServiceClass != attrs.ServiceClass || plan.Epoch != attrs.Epoch ||
		plan.PQRequirement != pol.PQRequirement {
		return model.RoutePlan{}, ErrRouteContext
	}
	if plan.Expired(now) {
		return model.RoutePlan{}, ErrRouteExpired
	}
	if len(plan.Gateways) == 0 {
		return model.RoutePlan{}, ErrNoRoute
	}
	if len(plan.Gateways) > maxRouteGateways {
		return model.RoutePlan{}, ErrRouteContext
	}
	return plan, nil
}

func (a *Agent) connectGateway(
	grant entitlement.SignedGrant,
	attrs capability.Attributes,
	pol model.PolicyBundle,
	gw model.RouteGateway,
) (Session, error) {
	tok, err := a.acquireCapability(grant, attrs)
	if err != nil {
		return Session{}, err
	}
	if err := a.deps.Admit(tok, gw); err != nil {
		return Session{}, err
	}
	status, err := a.deps.Tunnel.Up(tunnel.Config{
		GatewayID:          gw.GatewayID,
		GatewayEndpoint:    gw.Endpoint,
		GatewayWGPublicKey: gw.WireGuardPublicKey,
		DNS:                pol.DNS,
		PQRequirement:      pol.PQRequirement,
	})
	if err != nil {
		return Session{}, a.cleanupTunnel(err)
	}
	if !status.Up || status.GatewayID != gw.GatewayID {
		return Session{}, a.cleanupTunnel(ErrTunnelState)
	}
	if pol.PQRequirement == model.PQRequired && status.NegotiatedPQ != model.PQRequired {
		return Session{}, a.cleanupTunnel(ErrPQDowngrade)
	}
	a.killSwitch = pol.KillSwitch
	return Session{
		GatewayID:    gw.GatewayID,
		ServiceClass: attrs.ServiceClass,
		NegotiatedPQ: status.NegotiatedPQ,
		KillSwitch:   a.killSwitch,
	}, nil
}

func (a *Agent) cleanupTunnel(cause error) error {
	if err := a.deps.Tunnel.Down(); err != nil {
		return errors.Join(cause, fmt.Errorf("%w: %v", ErrTunnelCleanup, err))
	}
	return cause
}

// acquireCapability mints one fresh, unlinkable capability for attrs using the
// entitlement grant: blind -> issue -> finalize.
func (a *Agent) acquireCapability(grant entitlement.SignedGrant, attrs capability.Attributes) (*capability.Token, error) {
	keyID, pub, err := a.deps.IssuerPublicKey(grant, attrs)
	if err != nil {
		return nil, err
	}
	bl, err := capability.Prepare(keyID, pub, attrs)
	if err != nil {
		return nil, err
	}
	_, blindSig, err := a.deps.IssueCapability(capissuer.IssueRequest{
		Grant:   grant,
		Attrs:   attrs,
		Blinded: bl.Blinded,
	})
	if err != nil {
		return nil, err
	}
	return bl.Finalize(blindSig)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
