// Package integration exercises the Phase-1 vertical slice end to end
// (Integration Document Immediate Engineering Action #4): an administrator
// creates a tenant, enrols a device, assigns policy, and the device connects
// through a Haechi gateway, driving every owned component through its port with
// the dev adapters. This is the executable definition of "working core first".
package integration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	agentcore "github.com/QuantmindSSI/haechi/agent/core"
	"github.com/QuantmindSSI/haechi/agent/tunnel"
	"github.com/QuantmindSSI/haechi/internal/epoch"
	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
	"github.com/QuantmindSSI/haechi/pkg/capability"
	capissuer "github.com/QuantmindSSI/haechi/services/capability-issuer"
	capverifier "github.com/QuantmindSSI/haechi/services/capability-verifier"
	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
	"github.com/QuantmindSSI/haechi/services/entitlement"
	gatewayctl "github.com/QuantmindSSI/haechi/services/gateway-controller"
	routeorch "github.com/QuantmindSSI/haechi/services/route-orchestrator"
)

// ctx is the background context for tests; production paths carry a request context.
var ctx = context.Background()

const (
	audience     = "eu-standard"
	serviceClass = "general"
	rsaBits      = 2048
)

// slice holds a fully wired dev environment.
type slice struct {
	control  *controlapi.Service
	ent      *entitlement.Service
	issuer   *capissuer.Service
	orch     *routeorch.Service
	tenant   model.Tenant
	device   model.Device
	devKey   signing.PrivateKey
	subject  string
	loopback *tunnel.Loopback
}

func wgKeyB64(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func setup(t *testing.T) *slice {
	t.Helper()

	// --- Control plane: state (MemoryStore) + authz (StaticEngine) + signer ---
	controlSigner, _ := signing.Generate()
	control := controlapi.NewService(controlapi.NewMemoryStore(), nil, controlSigner, "control-api-key-1")

	// --- Identity plane: entitlement + static OIDC verifier ---
	entSigner, _ := signing.Generate()
	verifier := entitlement.NewStaticVerifier()
	ent := entitlement.NewService(entSigner, "entitlement-key-1", verifier)

	// --- Capability issuer trusts the entitlement key ---
	issuer := capissuer.NewService(ent.Pub(), rsaBits)

	// --- Route orchestrator reads gateways from control-api, signs plans ---
	orchSigner, _ := signing.Generate()
	orch := routeorch.NewService(control, orchSigner, "route-key-1")

	// --- Admin actions: tenant, policy, gateway ---
	tenant, err := control.CreateTenant(ctx, "Acme", "https://keycloak/realms/acme")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := control.SetTenantPolicy(ctx, tenant.ID, controlapi.TenantPolicy{
		AllowedServiceClasses: []string{serviceClass},
		AllowedTransports:     []model.Transport{model.TransportDirect},
		PQRequirement:         model.PQPreferred,
		KillSwitch:            true,
		DNS:                   []string{"10.0.0.53"},
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	if err := control.RegisterGateway(ctx, model.Gateway{
		ID: "gw-eu-1", TenantID: tenant.ID, Region: "eu-west", Audience: audience,
		ServiceClasses: []string{serviceClass}, Endpoint: "gw-eu-1.haechi.net:51820",
		WireGuardPublicKey: wgKeyB64(t), PQCapable: true,
	}); err != nil {
		t.Fatalf("register gateway: %v", err)
	}
	if err := control.PublishGatewayHealth(ctx, "gw-eu-1", true, 0.2); err != nil {
		t.Fatalf("health: %v", err)
	}

	// --- Device enrolment with proof-of-possession ---
	code, _ := control.IssueEnrollmentCode(ctx, tenant.ID)
	devKey, _ := signing.Generate()
	proof, _ := devKey.Sign(signing.ContextEnrollmentChallenge,
		controlapi.EnrollmentProofMessage(devKey.Public(), code.Challenge))
	device, err := control.EnrollDevice(ctx, controlapi.EnrollRequest{
		Code: code.Code, PublicKey: devKey.Public(), Platform: "linux", Proof: proof,
	})
	if err != nil {
		t.Fatalf("enrol: %v", err)
	}

	// --- Identity: register the enterprise subject and its entitlement ---
	subject := "user-1@acme"
	verifier.Add(subject, entitlement.Identity{
		TenantID: tenant.ID, Subject: subject,
		Entitlements: []string{audience + "|" + serviceClass},
	})

	return &slice{
		control: control, ent: ent, issuer: issuer, orch: orch,
		tenant: tenant, device: device, devKey: devKey, subject: subject,
		loopback: &tunnel.Loopback{PQAvailable: true},
	}
}

// buildAgent wires the agent Deps to the in-process services (composition root).
func (s *slice) buildAgent(t *testing.T) *agentcore.Agent {
	t.Helper()
	trust := agentcore.Trust{
		ControlPlanePub: s.control.SignerPub(),
		RoutePlanPub:    s.orch.Pub(),
	}
	deps := agentcore.Deps{
		FetchPolicy: func(tenantID string) (model.SignedPolicyBundle, error) {
			return s.control.PolicyBundle(ctx, tenantID)
		},
		Entitle: func(aud, sc string) (entitlement.SignedGrant, error) {
			// Layered enforcement: device authorisation (control/OPA) AND
			// subject entitlement (identity/Keycloak) before any capability.
			if dec := s.control.Authorize(ctx, s.device.ID, sc); !dec.Allow {
				return entitlement.SignedGrant{}, errDenied(dec.Reason)
			}
			return s.ent.Issue(context.Background(), s.subject, aud, sc)
		},
		IssuerPublicKey: s.issuer.PublicKey,
		IssueCapability: s.issuer.Issue,
		ResolveRoute: func(tenantID, aud, sc string, pq model.PQMode) (model.SignedRoutePlan, error) {
			return s.orch.Resolve(ctx, routeorch.ResolveRequest{
				TenantID: tenantID, Audience: aud, ServiceClass: sc, PQRequirement: pq,
			})
		},
		Admit:  s.admit,
		Tunnel: s.loopback,
	}
	return agentcore.New(s.device.ID, s.tenant.ID, s.subject, s.devKey, trust, deps)
}

// admit models a gateway: it provisions a verifier from the issuer's published
// directory, admits the capability locally (no identity call), then brings up
// the tunnel.
func (s *slice) admit(tok *capability.Token, gw model.RouteGateway) error {
	v := capverifier.FromDirectory(audience, []string{serviceClass},
		epoch.Window(time.Now()), s.issuer.Directory(), capability.NewMemoryReplayCache())
	gc := gatewayctl.New(gw.GatewayID, audience, v, gatewayctl.NewEpochBudget(100))
	if _, err := gc.Admit(tok); err != nil {
		return err
	}
	return nil
}

func TestVerticalSliceHappyPath(t *testing.T) {
	s := setup(t)
	agent := s.buildAgent(t)

	sess, err := agent.Connect(audience, serviceClass)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if sess.GatewayID != "gw-eu-1" {
		t.Fatalf("connected to %q, want gw-eu-1", sess.GatewayID)
	}
	if !sess.KillSwitch {
		t.Fatal("kill switch must be engaged")
	}
	// Policy PQ=preferred and the loopback supports PQ -> hybrid negotiated.
	if sess.NegotiatedPQ != model.PQRequired {
		t.Fatalf("expected hybrid PQ, got %q", sess.NegotiatedPQ)
	}
	if !s.loopback.Status().Up {
		t.Fatal("tunnel should be up")
	}
}

func TestConnectDeniedForUnpermittedServiceClass(t *testing.T) {
	s := setup(t)
	agent := s.buildAgent(t)
	// "premium" is not in policy nor entitlement -> fail closed at policy check.
	if _, err := agent.Connect(audience, "premium"); err == nil {
		t.Fatal("expected denial for unpermitted service class")
	}
	if !agent.KillSwitchEngaged() {
		t.Fatal("kill switch must remain engaged after policy denial")
	}
}

func TestConnectFailsClosedOnRevokedDevice(t *testing.T) {
	s := setup(t)
	if err := s.control.RevokeDevice(ctx, s.device.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	agent := s.buildAgent(t)
	if _, err := agent.Connect(audience, serviceClass); err == nil {
		t.Fatal("revoked device must fail closed")
	}
	if s.loopback.Status().Up {
		t.Fatal("no tunnel may be established for a revoked device")
	}
}

func TestConnectFailsClosedOnTamperedPolicySignature(t *testing.T) {
	s := setup(t)
	// Wrong control-plane trust key -> policy signature check fails closed.
	badKey, _ := signing.Generate()
	trust := agentcore.Trust{ControlPlanePub: badKey.Public(), RoutePlanPub: s.orch.Pub()}
	deps := agentcore.Deps{
		FetchPolicy: func(tid string) (model.SignedPolicyBundle, error) { return s.control.PolicyBundle(ctx, tid) },
		Entitle: func(aud, sc string) (entitlement.SignedGrant, error) {
			return s.ent.Issue(context.Background(), s.subject, aud, sc)
		},
		IssuerPublicKey: s.issuer.PublicKey,
		IssueCapability: s.issuer.Issue,
		ResolveRoute: func(tid, aud, sc string, pq model.PQMode) (model.SignedRoutePlan, error) {
			return s.orch.Resolve(ctx, routeorch.ResolveRequest{TenantID: tid, Audience: aud, ServiceClass: sc, PQRequirement: pq})
		},
		Admit:  s.admit,
		Tunnel: s.loopback,
	}
	agent := agentcore.New(s.device.ID, s.tenant.ID, s.subject, s.devKey, trust, deps)
	if _, err := agent.Connect(audience, serviceClass); err != agentcore.ErrPolicySignature {
		t.Fatalf("want ErrPolicySignature, got %v", err)
	}
	if !agent.KillSwitchEngaged() {
		t.Fatal("kill switch must remain engaged after signature failure")
	}
}

type errDenied string

func (e errDenied) Error() string { return "denied: " + string(e) }
