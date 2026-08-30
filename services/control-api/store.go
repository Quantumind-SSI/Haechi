// Package controlapi is the tenant-scoped control plane (Integration Document
// §6.2): organisations, users, devices, gateways, routes, policies, releases
// and audit. It authenticates administrators (Keycloak in production), makes
// deny-by-default authorisation decisions (OPA in production) and holds durable
// state (PostgreSQL in production). Here the Store is an interface with an
// in-memory implementation so the vertical slice runs without external systems.
package controlapi

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantmindSSI/haechi/internal/model"
)

// Store errors.
var (
	ErrNotFound  = errors.New("controlapi: not found")
	ErrConflict  = errors.New("controlapi: conflict")
	ErrCodeUsed  = errors.New("controlapi: enrolment code already used")
	ErrCodeStale = errors.New("controlapi: enrolment code expired")
)

// EnrollmentCode is a single-use, short-lived device bootstrap credential with
// a server-chosen challenge for proof-of-possession.
type EnrollmentCode struct {
	Code      string
	TenantID  string
	Challenge []byte
	ExpiresAt time.Time
	Used      bool
}

// TenantPolicy is the per-tenant configuration compiled into signed policy
// bundles. Deny-by-default: anything not listed is refused.
type TenantPolicy struct {
	AllowedServiceClasses []string          `json:"allowed_service_classes"`
	AllowedTransports     []model.Transport `json:"allowed_transports"`
	PQRequirement         model.PQMode      `json:"pq_requirement"`
	KillSwitch            bool              `json:"kill_switch"`
	DNS                   []string          `json:"dns"`
	Version               uint64            `json:"-"` // monotonic; bumped on every change
}

// Store is the durable-state contract. A PostgreSQL implementation with
// tenant-scoped queries is the production target; MemoryStore is used for
// development and tests.
//
// Every method takes a context. A control-plane request that outlives its
// caller is a request holding a connection nobody is waiting for, so the
// deadline has to reach the driver rather than stopping at the HTTP layer.
type Store interface {
	CreateTenant(ctx context.Context, t model.Tenant, initialPolicy TenantPolicy) error
	GetTenant(ctx context.Context, id string) (model.Tenant, error)

	PutEnrollmentCode(ctx context.Context, c EnrollmentCode) error
	GetEnrollmentCode(ctx context.Context, code string) (EnrollmentCode, error)
	ConsumeEnrollmentCode(ctx context.Context, code string, now time.Time, d model.Device) error
	GetDevice(ctx context.Context, id string) (model.Device, error)
	RevokeDevice(ctx context.Context, id string, at time.Time) error

	UpsertGateway(ctx context.Context, g model.Gateway) error
	GetGateway(ctx context.Context, id string) (model.Gateway, error)
	ListGateways(ctx context.Context, tenantID string) ([]model.Gateway, error)

	ReplaceTenantPolicy(ctx context.Context, tenantID string, p TenantPolicy) error
	GetTenantPolicy(ctx context.Context, tenantID string) (TenantPolicy, error)

	// Ping reports whether the store is reachable. Readiness depends on it, so
	// a revision that cannot reach its database never receives traffic.
	Ping(ctx context.Context) error
}

// MemoryStore is a concurrency-safe in-memory Store.
type MemoryStore struct {
	mu       sync.RWMutex
	tenants  map[string]model.Tenant
	codes    map[string]EnrollmentCode
	devices  map[string]model.Device
	gateways map[string]model.Gateway
	policies map[string]TenantPolicy
}

// NewMemoryStore constructs an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tenants:  map[string]model.Tenant{},
		codes:    map[string]EnrollmentCode{},
		devices:  map[string]model.Device{},
		gateways: map[string]model.Gateway{},
		policies: map[string]TenantPolicy{},
	}
}

func (s *MemoryStore) CreateTenant(_ context.Context, t model.Tenant, initialPolicy TenantPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[t.ID]; ok {
		return ErrConflict
	}
	initialPolicy.Version = 1
	s.tenants[t.ID] = t
	s.policies[t.ID] = initialPolicy
	return nil
}

func (s *MemoryStore) GetTenant(_ context.Context, id string) (model.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[id]
	if !ok {
		return model.Tenant{}, ErrNotFound
	}
	return t, nil
}

func (s *MemoryStore) PutEnrollmentCode(_ context.Context, c EnrollmentCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.codes[c.Code]; exists {
		return ErrConflict
	}
	s.codes[c.Code] = c
	return nil
}

func (s *MemoryStore) GetEnrollmentCode(_ context.Context, code string) (EnrollmentCode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.codes[code]
	if !ok {
		return EnrollmentCode{}, ErrNotFound
	}
	return c, nil
}

func (s *MemoryStore) ConsumeEnrollmentCode(_ context.Context, code string, now time.Time, d model.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[code]
	if !ok {
		return ErrNotFound
	}
	if c.Used {
		return ErrCodeUsed
	}
	if now.After(c.ExpiresAt) {
		return ErrCodeStale
	}
	if d.TenantID != c.TenantID {
		return ErrConflict
	}
	if _, ok := s.devices[d.ID]; ok {
		return ErrConflict
	}
	c.Used = true
	s.codes[code] = c
	s.devices[d.ID] = d
	return nil
}

func (s *MemoryStore) GetDevice(_ context.Context, id string) (model.Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[id]
	if !ok {
		return model.Device{}, ErrNotFound
	}
	return d, nil
}

func (s *MemoryStore) RevokeDevice(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.Revoked = true
	d.RevokedAt = &at
	s.devices[id] = d
	return nil
}

func (s *MemoryStore) UpsertGateway(_ context.Context, g model.Gateway) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.gateways[g.ID]; ok && existing.TenantID != g.TenantID {
		return ErrConflict
	}
	s.gateways[g.ID] = g
	return nil
}

func (s *MemoryStore) GetGateway(_ context.Context, id string) (model.Gateway, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.gateways[id]
	if !ok {
		return model.Gateway{}, ErrNotFound
	}
	return g, nil
}

func (s *MemoryStore) ListGateways(_ context.Context, tenantID string) ([]model.Gateway, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Gateway, 0, len(s.gateways))
	for _, g := range s.gateways {
		if g.TenantID != tenantID {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *MemoryStore) ReplaceTenantPolicy(_ context.Context, tenantID string, p TenantPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[tenantID]; !ok {
		return ErrNotFound
	}
	p.Version = s.policies[tenantID].Version + 1
	s.policies[tenantID] = p
	return nil
}

func (s *MemoryStore) GetTenantPolicy(_ context.Context, tenantID string) (TenantPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[tenantID]
	if !ok {
		return TenantPolicy{}, ErrNotFound
	}
	return p, nil
}

// Ping implements Store. Memory is always reachable.
func (s *MemoryStore) Ping(_ context.Context) error { return nil }
