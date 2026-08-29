package agentcore

import (
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/QuantmindSSI/haechi/agent/tunnel"
	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
	"github.com/QuantmindSSI/haechi/pkg/capability"
	capissuer "github.com/QuantmindSSI/haechi/services/capability-issuer"
	"github.com/QuantmindSSI/haechi/services/entitlement"
)

type fakeTunnel struct {
	status    tunnel.Status
	upStatus  tunnel.Status
	upErr     error
	downErr   error
	downCalls int
}

func (f *fakeTunnel) Up(tunnel.Config) (tunnel.Status, error) {
	if f.upErr != nil {
		return tunnel.Status{}, f.upErr
	}
	f.status = f.upStatus
	return f.status, nil
}

func (f *fakeTunnel) Down() error {
	f.downCalls++
	if f.downErr != nil {
		return f.downErr
	}
	f.status = tunnel.Status{}
	return nil
}

func (f *fakeTunnel) Status() tunnel.Status { return f.status }

func TestNewAgentStartsFailClosed(t *testing.T) {
	agent := New("device", "tenant", "subject", signing.PrivateKey{}, Trust{}, Deps{})
	if !agent.KillSwitchEngaged() {
		t.Fatal("new agent must start with its kill switch engaged")
	}
}

func TestConnectRejectsCrossTenantPolicy(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	controlKey := mustSigningKey(t)
	routeKey := mustSigningKey(t)
	policy := mustPolicy(t, controlKey, model.PolicyBundle{
		TenantID:              "other-tenant",
		Version:               1,
		AllowedServiceClasses: []string{"general"},
		IssuedAt:              now,
		ExpiresAt:             now.Add(time.Hour),
	})
	tun := &fakeTunnel{}
	deps := completeDeps(tun)
	deps.FetchPolicy = func(string) (model.SignedPolicyBundle, error) { return policy, nil }
	agent := New("device", "tenant", "subject", signing.PrivateKey{}, Trust{
		ControlPlanePub: controlKey.Public(), RoutePlanPub: routeKey.Public(),
	}, deps)
	agent.SetClock(func() time.Time { return now })

	if _, err := agent.Connect("eu-standard", "general"); !errors.Is(err, ErrPolicyTenant) {
		t.Fatalf("want ErrPolicyTenant, got %v", err)
	}
	if !agent.KillSwitchEngaged() || tun.Status().Up {
		t.Fatal("cross-tenant policy failure did not remain fail closed")
	}
}

func TestConnectRejectsRouteContextMismatch(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	controlKey := mustSigningKey(t)
	routeKey := mustSigningKey(t)
	policy := mustPolicy(t, controlKey, validPolicy(now, model.PQPreferred))
	route := mustRoute(t, routeKey, model.RoutePlan{
		TenantID:      "tenant",
		Audience:      "wrong-audience",
		ServiceClass:  "general",
		Epoch:         42,
		PQRequirement: model.PQPreferred,
		Gateways:      []model.RouteGateway{{GatewayID: "gw-1"}},
		IssuedAt:      now,
		ExpiresAt:     now.Add(time.Minute),
	})
	tun := &fakeTunnel{}
	deps := completeDeps(tun)
	deps.FetchPolicy = func(string) (model.SignedPolicyBundle, error) { return policy, nil }
	deps.Entitle = func(string, string) (entitlement.SignedGrant, error) {
		return validGrant(now), nil
	}
	deps.ResolveRoute = func(string, string, string, model.PQMode) (model.SignedRoutePlan, error) {
		return route, nil
	}
	agent := New("device", "tenant", "subject", signing.PrivateKey{}, Trust{
		ControlPlanePub: controlKey.Public(), RoutePlanPub: routeKey.Public(),
	}, deps)
	agent.SetClock(func() time.Time { return now })

	if _, err := agent.Connect("eu-standard", "general"); !errors.Is(err, ErrRouteContext) {
		t.Fatalf("want ErrRouteContext, got %v", err)
	}
	if !agent.KillSwitchEngaged() || tun.Status().Up {
		t.Fatal("route mismatch did not remain fail closed")
	}
}

func TestPQDowngradeTearsDownTunnel(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	controlKey := mustSigningKey(t)
	routeKey := mustSigningKey(t)
	issuer := capability.NewIssuer(2048)
	policy := mustPolicy(t, controlKey, validPolicy(now, model.PQRequired))
	route := mustRoute(t, routeKey, model.RoutePlan{
		TenantID:      "tenant",
		Audience:      "eu-standard",
		ServiceClass:  "general",
		Epoch:         42,
		PQRequirement: model.PQRequired,
		Gateways:      []model.RouteGateway{{GatewayID: "gw-1"}},
		IssuedAt:      now,
		ExpiresAt:     now.Add(time.Minute),
	})
	tun := &fakeTunnel{upStatus: tunnel.Status{Up: true, GatewayID: "gw-1", NegotiatedPQ: model.PQOff}}
	deps := completeDeps(tun)
	deps.FetchPolicy = func(string) (model.SignedPolicyBundle, error) { return policy, nil }
	deps.Entitle = func(string, string) (entitlement.SignedGrant, error) { return validGrant(now), nil }
	deps.ResolveRoute = func(string, string, string, model.PQMode) (model.SignedRoutePlan, error) {
		return route, nil
	}
	deps.IssuerPublicKey = func(_ entitlement.SignedGrant, attrs capability.Attributes) (capability.KeyID, *rsa.PublicKey, error) {
		return issuer.PublicKey(attrs)
	}
	deps.IssueCapability = func(req capissuer.IssueRequest) (capability.KeyID, []byte, error) {
		return issuer.BlindSign(req.Attrs, req.Blinded)
	}
	agent := New("device", "tenant", "subject", signing.PrivateKey{}, Trust{
		ControlPlanePub: controlKey.Public(), RoutePlanPub: routeKey.Public(),
	}, deps)
	agent.SetClock(func() time.Time { return now })

	if _, err := agent.Connect("eu-standard", "general"); !errors.Is(err, ErrPQDowngrade) {
		t.Fatalf("want ErrPQDowngrade, got %v", err)
	}
	if tun.Status().Up {
		t.Fatal("downgraded tunnel remained up")
	}
	if tun.downCalls != 2 {
		t.Fatalf("tunnel down calls = %d, want 2", tun.downCalls)
	}
}

func completeDeps(tun tunnel.Tunnel) Deps {
	return Deps{
		FetchPolicy: func(string) (model.SignedPolicyBundle, error) {
			return model.SignedPolicyBundle{}, errors.New("unexpected policy call")
		},
		Entitle: func(string, string) (entitlement.SignedGrant, error) {
			return entitlement.SignedGrant{}, errors.New("unexpected entitlement call")
		},
		IssuerPublicKey: func(entitlement.SignedGrant, capability.Attributes) (capability.KeyID, *rsa.PublicKey, error) {
			return "", nil, errors.New("unexpected issuer key call")
		},
		IssueCapability: func(capissuer.IssueRequest) (capability.KeyID, []byte, error) {
			return "", nil, errors.New("unexpected issuance call")
		},
		ResolveRoute: func(string, string, string, model.PQMode) (model.SignedRoutePlan, error) {
			return model.SignedRoutePlan{}, errors.New("unexpected route call")
		},
		Admit:  func(*capability.Token, model.RouteGateway) error { return nil },
		Tunnel: tun,
	}
}

func validPolicy(now time.Time, pq model.PQMode) model.PolicyBundle {
	return model.PolicyBundle{
		TenantID:              "tenant",
		Version:               1,
		KillSwitch:            true,
		AllowedServiceClasses: []string{"general"},
		PQRequirement:         pq,
		IssuedAt:              now,
		ExpiresAt:             now.Add(time.Hour),
	}
}

func validGrant(now time.Time) entitlement.SignedGrant {
	return entitlement.SignedGrant{Grant: entitlement.Grant{
		TenantID:     "tenant",
		Subject:      "subject",
		Audience:     "eu-standard",
		ServiceClass: "general",
		Epoch:        42,
		IssuedAt:     now,
		ExpiresAt:    now.Add(time.Hour),
	}}
}

func mustSigningKey(t *testing.T) signing.PrivateKey {
	t.Helper()
	key, err := signing.Generate()
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	return key
}

func mustPolicy(t *testing.T, key signing.PrivateKey, policy model.PolicyBundle) model.SignedPolicyBundle {
	t.Helper()
	signed, err := model.SignPolicyBundle(key, "control-key", policy)
	if err != nil {
		t.Fatalf("sign policy: %v", err)
	}
	return signed
}

func mustRoute(t *testing.T, key signing.PrivateKey, route model.RoutePlan) model.SignedRoutePlan {
	t.Helper()
	signed, err := model.SignRoutePlan(key, "route-key", route)
	if err != nil {
		t.Fatalf("sign route: %v", err)
	}
	return signed
}
