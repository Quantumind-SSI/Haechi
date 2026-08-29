// Package capissuer is the Haechi capability issuer (Integration Document §6.3,
// §7 IssueAccessCapability). It requires a valid entitlement grant from the
// identity plane, checks the requested coarse attributes match the grant and
// the current epoch, then blind-signs the endpoint's blinded capability. Because
// issuance is blind, the issuer cannot link the grant to any later gateway
// presentation.
package capissuer

import (
	"crypto/rsa"
	"errors"
	"time"

	"github.com/QuantmindSSI/haechi/internal/signing"
	"github.com/QuantmindSSI/haechi/pkg/capability"
	"github.com/QuantmindSSI/haechi/services/entitlement"
)

// Errors all deny issuance.
var (
	ErrBadGrant     = errors.New("capissuer: entitlement grant invalid or expired")
	ErrAttrMismatch = errors.New("capissuer: requested attributes do not match grant")
)

// Service wraps the capability.Issuer with entitlement enforcement.
type Service struct {
	issuer *capability.Issuer
	entPub signing.PublicKey
	now    func() time.Time
}

// NewService constructs the issuer service. entPub is the entitlement-service
// public key used to verify grants. bits is the RSA modulus size.
func NewService(entPub signing.PublicKey, bits int) *Service {
	return &Service{issuer: capability.NewIssuer(bits), entPub: entPub, now: time.Now}
}

// SetClock overrides the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// PublicKey verifies the entitlement grant before returning or creating the key
// used to blind a capability for the requested attributes.
func (s *Service) PublicKey(
	grant entitlement.SignedGrant,
	attrs capability.Attributes,
) (capability.KeyID, *rsa.PublicKey, error) {
	if err := s.validateGrant(grant, attrs); err != nil {
		return "", nil, err
	}
	return s.issuer.PublicKey(attrs)
}

// Directory publishes the verifier keyset for gateways.
func (s *Service) Directory() []capability.DirectoryEntry { return s.issuer.Directory() }

// IssueRequest carries the entitlement grant, requested attributes and the
// client's blinded capability.
type IssueRequest struct {
	Grant   entitlement.SignedGrant
	Attrs   capability.Attributes
	Blinded []byte
}

// Issue verifies the grant, binds attributes and blind-signs. It returns the
// key id and blind signature the client will finalize into a Token.
func (s *Service) Issue(req IssueRequest) (capability.KeyID, []byte, error) {
	if err := s.validateGrant(req.Grant, req.Attrs); err != nil {
		return "", nil, err
	}
	return s.issuer.BlindSign(req.Attrs, req.Blinded)
}

func (s *Service) validateGrant(grant entitlement.SignedGrant, attrs capability.Attributes) error {
	if !grant.Verify(s.entPub, s.now()) {
		return ErrBadGrant
	}
	g := grant.Grant
	if attrs.Audience != g.Audience || attrs.ServiceClass != g.ServiceClass || attrs.Epoch != g.Epoch {
		return ErrAttrMismatch
	}
	return nil
}
