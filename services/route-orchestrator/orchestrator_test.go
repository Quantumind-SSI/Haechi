package routeorch

import (
	"fmt"
	"testing"

	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
)

type gatewaySource []model.Gateway

func (g gatewaySource) EligibleGateways(string, string, string) ([]model.Gateway, error) {
	return g, nil
}

func TestRequiredPQExcludesIncapableGateways(t *testing.T) {
	signer, err := signing.Generate()
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	svc := NewService(gatewaySource{
		{ID: "classical", PQCapable: false, Load: 0.1},
		{ID: "hybrid", PQCapable: true, Load: 0.2},
	}, signer, "route-key")

	plan, err := svc.Resolve(ResolveRequest{
		TenantID: "tenant", Audience: "eu-standard", ServiceClass: "general", PQRequirement: model.PQRequired,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Plan.Gateways) != 1 || plan.Plan.Gateways[0].GatewayID != "hybrid" {
		t.Fatalf("gateways = %+v, want only hybrid", plan.Plan.Gateways)
	}
	plan.SignerKeyID = "substituted-key"
	if plan.Verify(signer.Public()) {
		t.Fatal("route with substituted signer key id verified")
	}
}

func TestRequiredPQFailsWithoutCapableGateway(t *testing.T) {
	signer, err := signing.Generate()
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	svc := NewService(gatewaySource{{ID: "classical"}}, signer, "route-key")
	if _, err := svc.Resolve(ResolveRequest{PQRequirement: model.PQRequired}); err != ErrNoEligibleGateways {
		t.Fatalf("want ErrNoEligibleGateways, got %v", err)
	}
}

func TestRoutePlanGatewayCountIsBounded(t *testing.T) {
	signer, err := signing.Generate()
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	gateways := make(gatewaySource, maxRouteGateways+5)
	for i := range gateways {
		gateways[i] = model.Gateway{ID: fmt.Sprintf("gw-%02d", i), Load: float64(i) / 100}
	}
	svc := NewService(gateways, signer, "route-key")
	plan, err := svc.Resolve(ResolveRequest{PQRequirement: model.PQOff})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Plan.Gateways) != maxRouteGateways {
		t.Fatalf("gateway count = %d, want %d", len(plan.Plan.Gateways), maxRouteGateways)
	}
}
