// Package routeorch is the Haechi route orchestrator (Integration Document §6.4,
// §7 ResolveRoute). It selects eligible gateways from policy, region, health and
// capacity and returns a SIGNED route plan carrying no durable user identity.
// The endpoint validates the signature and expiry, then chooses locally.
package routeorch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/QuantmindSSI/haechi/internal/epoch"
	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
)

var (
	ErrInvalidPQMode      = errors.New("routeorch: invalid post-quantum mode")
	ErrNoEligibleGateways = errors.New("routeorch: no gateway satisfies route policy")
)

const maxRouteGateways = 16

// GatewaySource supplies eligible gateways. Implemented by the control-api.
type GatewaySource interface {
	EligibleGateways(ctx context.Context, tenantID, audience, serviceClass string) ([]model.Gateway, error)
}

// Service produces signed route plans.
type Service struct {
	src    GatewaySource
	signer signing.Signer
	keyID  string
	now    func() time.Time
	ttl    time.Duration
}

// NewService constructs the orchestrator. signer signs route plans (in-process
// key in dev, OpenBao Transit in prod); the agent trusts Pub() out of band.
func NewService(src GatewaySource, signer signing.Signer, keyID string) *Service {
	return &Service{src: src, signer: signer, keyID: keyID, now: time.Now, ttl: 5 * time.Minute}
}

// SetClock overrides the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Pub returns the route-plan verification key.
func (s *Service) Pub() signing.PublicKey { return s.signer.Public() }

// ResolveRequest asks for a route for coarse attributes. It carries the policy's
// PQ requirement so the plan can declare it (ADR-005).
type ResolveRequest struct {
	TenantID      string
	Audience      string
	ServiceClass  string
	PQRequirement model.PQMode
}

// Resolve returns a signed route plan of eligible gateways ordered by score.
func (s *Service) Resolve(ctx context.Context, req ResolveRequest) (model.SignedRoutePlan, error) {
	pq := req.PQRequirement
	if pq == "" {
		pq = model.PQPreferred
	}
	if pq != model.PQOff && pq != model.PQPreferred && pq != model.PQRequired {
		return model.SignedRoutePlan{}, ErrInvalidPQMode
	}
	gws, err := s.src.EligibleGateways(ctx, req.TenantID, req.Audience, req.ServiceClass)
	if err != nil {
		return model.SignedRoutePlan{}, err
	}

	scored := make([]model.RouteGateway, 0, len(gws))
	for _, g := range gws {
		if pq == model.PQRequired && !g.PQCapable {
			continue
		}
		scored = append(scored, model.RouteGateway{
			GatewayID:          g.ID,
			Region:             g.Region,
			Endpoint:           g.Endpoint,
			WireGuardPublicKey: g.WireGuardPublicKey,
			Score:              score(g),
		})
	}
	if len(scored) == 0 {
		return model.SignedRoutePlan{}, ErrNoEligibleGateways
	}
	// Highest score first; stable tie-break by gateway id for determinism.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].GatewayID < scored[j].GatewayID
	})
	if len(scored) > maxRouteGateways {
		scored = scored[:maxRouteGateways]
	}
	for i := range scored {
		scored[i].Priority = i
	}

	now := s.now()
	planID, err := randID()
	if err != nil {
		return model.SignedRoutePlan{}, err
	}
	plan := model.RoutePlan{
		PlanID:        planID,
		TenantID:      req.TenantID,
		Audience:      req.Audience,
		ServiceClass:  req.ServiceClass,
		Epoch:         epoch.At(now),
		PQRequirement: pq,
		Gateways:      scored,
		IssuedAt:      now,
		ExpiresAt:     now.Add(s.ttl),
	}
	return model.SignRoutePlan(s.signer, s.keyID, plan)
}

func randID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("routeorch: generate plan id: %w", err)
	}
	return "rp_" + hex.EncodeToString(b), nil
}

// score ranks a gateway: prefer low load. Range (0,1].
func score(g model.Gateway) float64 {
	load := g.Load
	if load < 0 {
		load = 0
	}
	if load > 1 {
		load = 1
	}
	return 1.0 - load
}
