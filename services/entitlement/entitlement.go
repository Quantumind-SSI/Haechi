// Package entitlement is the identity-plane service (Integration Document §4.1,
// §6.3). It verifies an authenticated enterprise subject through the
// IdentityVerifier port (Keycloak/OIDC in production) and converts a proven
// entitlement into a short-lived, signed grant. The grant is consumed ONLY by
// the capability issuer and never reaches a gateway, keeping identity and
// gateway session in separate trust zones.
package entitlement

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantmindSSI/haechi/internal/epoch"
	"github.com/QuantmindSSI/haechi/internal/signing"
)

// Errors.
var (
	ErrNotEntitled     = errors.New("entitlement: subject not entitled to requested scope")
	ErrUnauthenticated = errors.New("entitlement: identity verification failed")
)

// Identity is the verified result of an authentication token. Subject is an
// opaque IdP identifier visible to the identity plane and issuer, never to a
// gateway. Entitlements are coarse "audience|service_class" grants.
type Identity struct {
	TenantID     string
	Subject      string
	Entitlements []string
}

// Entitled reports whether the identity may request (audience, serviceClass).
func (id Identity) Entitled(audience, serviceClass string) bool {
	if audience == "" || serviceClass == "" || strings.Contains(audience, "|") || strings.Contains(serviceClass, "|") {
		return false
	}
	want := audience + "|" + serviceClass
	for _, e := range id.Entitlements {
		if e == want {
			return true
		}
	}
	return false
}

// IdentityVerifier is the authentication PORT. The production adapter
// (adapters/keycloak) validates an OIDC token; StaticVerifier is the dev adapter.
type IdentityVerifier interface {
	// Verify authenticates a bearer token and returns the verified identity.
	Verify(ctx context.Context, token string) (Identity, error)
}

// Grant is the unsigned entitlement statement.
type Grant struct {
	TenantID     string    `json:"tenant_id"`
	Subject      string    `json:"subject"`
	Audience     string    `json:"audience"`
	ServiceClass string    `json:"service_class"`
	Epoch        uint64    `json:"epoch"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (g Grant) signingBytes() []byte {
	return []byte(g.TenantID + "\x1f" + g.Subject + "\x1f" + g.Audience + "\x1f" +
		g.ServiceClass + "\x1f" + itoa(g.Epoch) + "\x1f" +
		g.IssuedAt.UTC().Format(time.RFC3339Nano) + "\x1f" +
		g.ExpiresAt.UTC().Format(time.RFC3339Nano))
}

// SignedGrant is a grant with the entitlement-service signature.
type SignedGrant struct {
	Grant       Grant  `json:"grant"`
	SignerKeyID string `json:"signer_key_id"`
	Signature   []byte `json:"signature"`
}

// Verify checks the entitlement-service signature and expiry at now.
func (sg SignedGrant) Verify(pub signing.PublicKey, now time.Time) bool {
	if now.After(sg.Grant.ExpiresAt) {
		return false
	}
	return pub.Verify(signing.ContextEntitlementGrant, sg.Grant.signingBytes(), sg.Signature)
}

// Service issues signed entitlement grants.
type Service struct {
	signer   signing.Signer
	keyID    string
	verifier IdentityVerifier
	now      func() time.Time
}

// NewService constructs the entitlement service. signer is the in-process key in
// dev or the OpenBao Transit adapter in prod; verifier is StaticVerifier in dev
// or the Keycloak OIDC adapter in prod.
func NewService(signer signing.Signer, keyID string, verifier IdentityVerifier) *Service {
	return &Service{signer: signer, keyID: keyID, verifier: verifier, now: time.Now}
}

// SetClock overrides the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Pub returns the public key issuers use to verify grants.
func (s *Service) Pub() signing.PublicKey { return s.signer.Public() }

// Issue authenticates the token, checks entitlement and returns a signed grant
// bound to the current epoch. Fail closed on auth or entitlement failure.
func (s *Service) Issue(ctx context.Context, token, audience, serviceClass string) (SignedGrant, error) {
	id, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return SignedGrant{}, ErrUnauthenticated
	}
	if id.Subject == "" || id.TenantID == "" {
		return SignedGrant{}, ErrUnauthenticated
	}
	if !id.Entitled(audience, serviceClass) {
		return SignedGrant{}, ErrNotEntitled
	}
	now := s.now()
	g := Grant{
		TenantID:     id.TenantID,
		Subject:      id.Subject,
		Audience:     audience,
		ServiceClass: serviceClass,
		Epoch:        epoch.At(now),
		IssuedAt:     now,
		ExpiresAt:    now.Add(epoch.Length),
	}
	sig, err := s.signer.Sign(signing.ContextEntitlementGrant, g.signingBytes())
	if err != nil {
		return SignedGrant{}, err
	}
	return SignedGrant{Grant: g, SignerKeyID: s.keyID, Signature: sig}, nil
}

// StaticVerifier is the dev IdentityVerifier: it maps an opaque dev token to a
// fixed Identity. In production the Keycloak adapter replaces it.
type StaticVerifier struct {
	identities map[string]Identity
}

// NewStaticVerifier builds an empty dev verifier.
func NewStaticVerifier() *StaticVerifier {
	return &StaticVerifier{identities: map[string]Identity{}}
}

// Add registers a dev token -> identity mapping and returns the token.
func (v *StaticVerifier) Add(token string, id Identity) string {
	v.identities[token] = id
	return token
}

// Verify implements IdentityVerifier.
func (v *StaticVerifier) Verify(_ context.Context, token string) (Identity, error) {
	id, ok := v.identities[token]
	if !ok {
		return Identity{}, ErrUnauthenticated
	}
	return id, nil
}

func itoa(u uint64) string {
	if u == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}
