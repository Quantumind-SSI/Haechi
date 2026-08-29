// Package controlapi is the tenant-scoped control plane (Integration Document
// §6.2): organisations, users, devices, gateways, routes, policies, releases
// and audit. It authenticates administrators (Keycloak in production), makes
// deny-by-default authorisation decisions (OPA in production) and holds durable
// state (PostgreSQL in production). Here the Store is an interface with an
// in-memory implementation so the vertical slice runs without external systems.
package controlapi

import (
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
// row-level tenant isolation is the production target; MemoryStore is used for
// development and tests.
type Store interface {
	CreateTenant(t model.Tenant, initialPolicy TenantPolicy) error
	GetTenant(id string) (model.Tenant, error)

	PutEnrollmentCode(c EnrollmentCode) error
	GetEnrollmentCode(code string) (EnrollmentCode, error)
	ConsumeEnrollmentCode(code string, now time.Time, d model.Device) error
	GetDevice(id string) (model.Device, error)
	RevokeDevice(id string, at time.Time) error

	UpsertGateway(g model.Gateway) error
	GetGateway(id string) (model.Gateway, error)
	ListGateways(tenantID string) ([]model.Gateway, error)

	ReplaceTenantPolicy(tenantID string, p TenantPolicy) error
	GetTenantPolicy(tenantID string) (TenantPolicy, error)
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

func (s *MemoryStore) CreateTenant(t model.Tenant, initialPolicy TenantPolicy) error {
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

func (s *MemoryStore) GetTenant(id string) (model.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[id]
	if !ok {
		return model.Tenant{}, ErrNotFound
	}
	return t, nil
}

func (s *MemoryStore) PutEnrollmentCode(c EnrollmentCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.codes[c.Code]; exists {
		return ErrConflict
	}
	s.codes[c.Code] = c
	return nil
}

func (s *MemoryStore) GetEnrollmentCode(code string) (EnrollmentCode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.codes[code]
	if !ok {
		return EnrollmentCode{}, ErrNotFound
	}
	return c, nil
}

func (s *MemoryStore) ConsumeEnrollmentCode(code string, now time.Time, d model.Device) error {
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

func (s *MemoryStore) GetDevice(id string) (model.Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[id]
	if !ok {
		return model.Device{}, ErrNotFound
	}
	return d, nil
}

func (s *MemoryStore) RevokeDevice(id string, at time.Time) error {
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

func (s *MemoryStore) UpsertGateway(g model.Gateway) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.gateways[g.ID]; ok && existing.TenantID != g.TenantID {
		return ErrConflict
	}
	s.gateways[g.ID] = g
	return nil
}

func (s *MemoryStore) GetGateway(id string) (model.Gateway, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.gateways[id]
	if !ok {
		return model.Gateway{}, ErrNotFound
	}
	return g, nil
}

func (s *MemoryStore) ListGateways(tenantID string) ([]model.Gateway, error) {
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

func (s *MemoryStore) ReplaceTenantPolicy(tenantID string, p TenantPolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[tenantID]; !ok {
		return ErrNotFound
	}
	p.Version = s.policies[tenantID].Version + 1
	s.policies[tenantID] = p
	return nil
}

func (s *MemoryStore) GetTenantPolicy(tenantID string) (TenantPolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[tenantID]
	if !ok {
		return TenantPolicy{}, ErrNotFound
	}
	return p, nil
}
