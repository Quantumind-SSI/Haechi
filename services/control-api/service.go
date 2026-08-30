package controlapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/QuantmindSSI/haechi/internal/epoch"
	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
)

// Service-level errors.
var (
	ErrBadProof   = errors.New("controlapi: device proof-of-possession failed")
	ErrRevoked    = errors.New("controlapi: device revoked")
	ErrDenied     = errors.New("controlapi: denied by policy")
	ErrNoGateways = errors.New("controlapi: no eligible gateways")
	ErrInvalid    = errors.New("controlapi: invalid input")
)

// Service implements control-plane behaviour over its ports: a Store (state),
// a PolicyEngine (authorisation) and a Signer (bundle signatures). The agent
// trusts SignerPub() out of band.
type Service struct {
	store       Store
	engine      PolicyEngine
	signer      signing.Signer
	signerKeyID string
	now         func() time.Time
	codeTTL     time.Duration
}

// NewService constructs the control-api service. engine may be nil, in which
// case the deny-by-default StaticEngine is used (dev). In production engine is
// the OPA adapter and signer is the OpenBao adapter.
func NewService(store Store, engine PolicyEngine, signer signing.Signer, signerKeyID string) *Service {
	if engine == nil {
		engine = StaticEngine{}
	}
	return &Service{
		store:       store,
		engine:      engine,
		signer:      signer,
		signerKeyID: signerKeyID,
		now:         time.Now,
		codeTTL:     15 * time.Minute,
	}
}

// SetClock overrides the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SignerPub is the control-plane public key that verifies policy bundles.
func (s *Service) SignerPub() signing.PublicKey { return s.signer.Public() }

// SignerKeyID identifies the signing key in signed bundles.
func (s *Service) SignerKeyID() string { return s.signerKeyID }

// CreateTenant provisions an enterprise boundary.
func (s *Service) CreateTenant(ctx context.Context, name, idpIssuer string) (model.Tenant, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return model.Tenant{}, fmt.Errorf("%w: tenant name", ErrInvalid)
	}
	idpIssuer = strings.TrimSpace(idpIssuer)
	if idpIssuer != "" {
		issuerURL, err := url.ParseRequestURI(idpIssuer)
		if err != nil || issuerURL.Host == "" || (issuerURL.Scheme != "https" && issuerURL.Scheme != "http") || len(idpIssuer) > 2048 {
			return model.Tenant{}, fmt.Errorf("%w: identity provider issuer", ErrInvalid)
		}
	}
	id, err := randID("ten")
	if err != nil {
		return model.Tenant{}, err
	}
	t := model.Tenant{ID: id, Name: name, IdPIssuer: idpIssuer, CreatedAt: s.now()}
	initialPolicy := TenantPolicy{
		AllowedServiceClasses: []string{},
		AllowedTransports:     []model.Transport{model.TransportDirect},
		PQRequirement:         model.PQPreferred,
		KillSwitch:            true,
		DNS:                   []string{},
	}
	if err := s.store.CreateTenant(ctx, t, initialPolicy); err != nil {
		return model.Tenant{}, err
	}
	return t, nil
}

// IssueEnrollmentCode mints a single-use bootstrap code and challenge.
func (s *Service) IssueEnrollmentCode(ctx context.Context, tenantID string) (EnrollmentCode, error) {
	if _, err := s.store.GetTenant(ctx, tenantID); err != nil {
		return EnrollmentCode{}, err
	}
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return EnrollmentCode{}, err
	}
	code, err := randCode()
	if err != nil {
		return EnrollmentCode{}, err
	}
	c := EnrollmentCode{
		Code:      code,
		TenantID:  tenantID,
		Challenge: challenge,
		ExpiresAt: s.now().Add(s.codeTTL),
	}
	stored := c
	stored.Code = enrollmentCodeKey(code)
	if err := s.store.PutEnrollmentCode(ctx, stored); err != nil {
		return EnrollmentCode{}, err
	}
	return c, nil
}

// EnrollRequest is the device enrolment payload (§7 EnrollDevice). The device
// proves possession of the private key matching PublicKey.
type EnrollRequest struct {
	Code      string
	PublicKey signing.PublicKey
	Platform  string
	Posture   string
	Proof     []byte
}

// EnrollmentProofMessage is the exact bytes a device must sign (challenge bound
// to the presented public key). Exported so agent and server agree byte-for-byte.
func EnrollmentProofMessage(pub signing.PublicKey, challenge []byte) []byte {
	msg := make([]byte, 0, len(challenge)+len(pub.Bytes()))
	msg = append(msg, challenge...)
	msg = append(msg, pub.Bytes()...)
	return msg
}

// EnrollDevice validates the code and proof-of-possession, then registers the
// device. Fail closed on any check.
func (s *Service) EnrollDevice(ctx context.Context, req EnrollRequest) (model.Device, error) {
	if req.Code == "" || len(req.PublicKey.Bytes()) != 32 || len(req.Proof) == 0 {
		return model.Device{}, fmt.Errorf("%w: enrollment credential", ErrInvalid)
	}
	codeKey := enrollmentCodeKey(req.Code)
	c, err := s.store.GetEnrollmentCode(ctx, codeKey)
	if err != nil {
		return model.Device{}, err
	}
	if c.Used {
		return model.Device{}, ErrCodeUsed
	}
	if s.now().After(c.ExpiresAt) {
		return model.Device{}, ErrCodeStale
	}
	msg := EnrollmentProofMessage(req.PublicKey, c.Challenge)
	if !req.PublicKey.Verify(signing.ContextEnrollmentChallenge, msg, req.Proof) {
		return model.Device{}, ErrBadProof
	}
	deviceID, err := randID("dev")
	if err != nil {
		return model.Device{}, err
	}
	d := model.Device{
		ID:         deviceID,
		TenantID:   c.TenantID,
		PublicKey:  req.PublicKey,
		Platform:   req.Platform,
		Posture:    req.Posture,
		EnrolledAt: s.now(),
	}
	if err := s.store.ConsumeEnrollmentCode(ctx, codeKey, s.now(), d); err != nil {
		return model.Device{}, err
	}
	return d, nil
}

// RevokeDevice marks a device revoked (fail closed on subsequent authorisation).
func (s *Service) RevokeDevice(ctx context.Context, id string) error {
	return s.store.RevokeDevice(ctx, id, s.now())
}

// RegisterGateway adds or updates a data-plane gateway.
func (s *Service) RegisterGateway(ctx context.Context, g model.Gateway) error {
	if err := validateGateway(g); err != nil {
		return err
	}
	if _, err := s.store.GetTenant(ctx, g.TenantID); err != nil {
		return err
	}
	g.LastSeen = s.now()
	return s.store.UpsertGateway(ctx, g)
}

// PublishGatewayHealth updates coarse gateway health (§7 PublishGatewayHealth).
func (s *Service) PublishGatewayHealth(ctx context.Context, id string, healthy bool, load float64) error {
	if id == "" || load < 0 || load > 1 {
		return fmt.Errorf("%w: gateway health", ErrInvalid)
	}
	g, err := s.store.GetGateway(ctx, id)
	if err != nil {
		return err
	}
	g.Healthy = healthy
	g.Load = load
	g.LastSeen = s.now()
	return s.store.UpsertGateway(ctx, g)
}

// SetTenantPolicy replaces tenant policy and bumps its monotonic version so
// endpoints get rollback-protected bundles.
func (s *Service) SetTenantPolicy(ctx context.Context, tenantID string, p TenantPolicy) error {
	if tenantID == "" {
		return fmt.Errorf("%w: tenant id", ErrInvalid)
	}
	if err := validatePolicy(p); err != nil {
		return err
	}
	return s.store.ReplaceTenantPolicy(ctx, tenantID, p)
}

// PolicyBundle compiles and signs the current endpoint policy for a tenant
// (§7 FetchPolicyBundle): versioned, signed, rollback-protected, cacheable.
func (s *Service) PolicyBundle(ctx context.Context, tenantID string) (model.SignedPolicyBundle, error) {
	tp, err := s.store.GetTenantPolicy(ctx, tenantID)
	if err != nil {
		return model.SignedPolicyBundle{}, err
	}
	now := s.now()
	bundleID, err := randID("pb")
	if err != nil {
		return model.SignedPolicyBundle{}, err
	}
	bundle := model.PolicyBundle{
		BundleID:              bundleID,
		TenantID:              tenantID,
		Version:               tp.Version,
		Epoch:                 epoch.At(now),
		KillSwitch:            tp.KillSwitch,
		AllowedServiceClasses: tp.AllowedServiceClasses,
		AllowedTransports:     tp.AllowedTransports,
		PQRequirement:         tp.PQRequirement,
		DNS:                   tp.DNS,
		IssuedAt:              now,
		ExpiresAt:             now.Add(2 * epoch.Length),
	}
	return model.SignPolicyBundle(s.signer, s.signerKeyID, bundle)
}

// Decision is a deny-by-default authorisation result.
type Decision struct {
	Allow  bool
	Reason string
}

// Authorize decides whether a device may use a service class. It loads state,
// builds the AuthzInput and delegates the decision to the PolicyEngine port
// (OPA in production, StaticEngine in dev). Any engine error fails closed.
func (s *Service) Authorize(ctx context.Context, deviceID, serviceClass string) Decision {
	d, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return Decision{false, "unknown device"}
	}
	tp, err := s.store.GetTenantPolicy(ctx, d.TenantID)
	if err != nil {
		return Decision{false, "no tenant policy"}
	}
	dec, err := s.engine.Authorize(ctx, AuthzInput{
		DeviceID:              d.ID,
		DeviceRevoked:         d.Revoked,
		TenantID:              d.TenantID,
		Platform:              d.Platform,
		Posture:               d.Posture,
		ServiceClass:          serviceClass,
		AllowedServiceClasses: tp.AllowedServiceClasses,
	})
	if err != nil {
		return Decision{false, "policy engine unavailable"} // fail closed
	}
	return dec
}

// EligibleGateways returns healthy gateways matching audience and service class
// for a tenant. Used by the route orchestrator.
func (s *Service) EligibleGateways(ctx context.Context, tenantID, audience, serviceClass string) ([]model.Gateway, error) {
	all, err := s.store.ListGateways(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var out []model.Gateway
	for _, g := range all {
		if !g.Healthy || g.Audience != audience || !contains(g.ServiceClasses, serviceClass) {
			continue
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil, ErrNoGateways
	}
	return out, nil
}

// TenantForDevice returns the tenant id a device belongs to.
func (s *Service) TenantForDevice(ctx context.Context, deviceID string) (string, error) {
	d, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return "", err
	}
	return d.TenantID, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func validateGateway(g model.Gateway) error {
	if !validIdentifier(g.ID) || !validIdentifier(g.TenantID) || !validIdentifier(g.Audience) {
		return fmt.Errorf("%w: gateway identity", ErrInvalid)
	}
	if g.Load < 0 || g.Load > 1 {
		return fmt.Errorf("%w: gateway load", ErrInvalid)
	}
	host, _, err := net.SplitHostPort(g.Endpoint)
	if err != nil || host == "" || strings.ContainsAny(g.Endpoint, "\r\n\x00") {
		return fmt.Errorf("%w: gateway endpoint", ErrInvalid)
	}
	key, err := base64.StdEncoding.DecodeString(g.WireGuardPublicKey)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("%w: WireGuard public key", ErrInvalid)
	}
	if err := validateStrings("service class", g.ServiceClasses); err != nil {
		return err
	}
	return nil
}

func validatePolicy(p TenantPolicy) error {
	if p.PQRequirement != model.PQOff && p.PQRequirement != model.PQPreferred && p.PQRequirement != model.PQRequired {
		return fmt.Errorf("%w: PQ requirement", ErrInvalid)
	}
	if err := validateStrings("service class", p.AllowedServiceClasses); err != nil {
		return err
	}
	if err := validateStrings("DNS server", p.DNS); err != nil {
		return err
	}
	if len(p.AllowedTransports) > 8 {
		return fmt.Errorf("%w: too many transports", ErrInvalid)
	}
	for _, transport := range p.AllowedTransports {
		if transport != model.TransportDirect && transport != model.TransportProtected {
			return fmt.Errorf("%w: transport", ErrInvalid)
		}
	}
	return nil
}

func validateStrings(field string, values []string) error {
	if len(values) > 64 {
		return fmt.Errorf("%w: too many %s values", ErrInvalid, field)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 253 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("%w: %s", ErrInvalid, field)
		}
		if field == "service class" && !validIdentifier(value) {
			return fmt.Errorf("%w: %s", ErrInvalid, field)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%w: duplicate %s", ErrInvalid, field)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func randID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}

func randCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func enrollmentCodeKey(code string) string {
	digest := sha256.Sum256([]byte(code))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// Ping reports whether durable state is reachable. Readiness depends on it.
func (s *Service) Ping(ctx context.Context) error { return s.store.Ping(ctx) }
