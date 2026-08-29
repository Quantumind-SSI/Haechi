// Package signing provides Ed25519 signatures with mandatory domain
// separation for every signed artefact in Haechi (route plans, policy
// bundles, gateway identity, enrolment proofs).
//
// Domain separation guarantees a signature produced for one context can never
// be reinterpreted as a valid signature in another context. Every Sign/Verify
// call MUST name its Context; there is no context-free signing API.
//
// Standard-library only (crypto/ed25519) per ADR-003: the fail-closed control
// and agent paths keep a small, auditable cryptographic surface.
package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
)

// Context is a domain-separation tag. Values are stable wire identifiers and
// MUST NOT be reused across artefact types.
type Context string

const (
	ContextRoutePlan           Context = "haechi/route-plan/v1"
	ContextPolicyBundle        Context = "haechi/policy-bundle/v1"
	ContextGatewayIdentity     Context = "haechi/gateway-identity/v1"
	ContextEnrollmentChallenge Context = "haechi/enrollment-challenge/v1"
	ContextDeviceProof         Context = "haechi/device-proof/v1"
	ContextGatewayHealth       Context = "haechi/gateway-health/v1"
	ContextEntitlementGrant    Context = "haechi/entitlement-grant/v1"
)

// ErrEmptyContext is returned if a caller attempts to sign or verify without a
// domain-separation context. Fail closed: never sign context-free data.
var ErrEmptyContext = errors.New("signing: empty domain-separation context")

// frame binds the context to the message with an unambiguous length prefix so
// that (ctxA, msgA) and (ctxB, msgB) can never collide.
func frame(ctx Context, msg []byte) ([]byte, error) {
	if ctx == "" {
		return nil, ErrEmptyContext
	}
	c := []byte(ctx)
	out := make([]byte, 0, 2+len(c)+len(msg))
	var lp [2]byte
	binary.BigEndian.PutUint16(lp[:], uint16(len(c)))
	out = append(out, lp[:]...)
	out = append(out, c...)
	out = append(out, msg...)
	return out, nil
}

// Signer is the signing PORT. The dev adapter is an in-process PrivateKey; the
// production adapter (adapters/openbao) delegates to OpenBao's Transit engine so
// private key material never resides in an application pod (§13 key management).
// Implementations MUST apply the same domain-separation framing as PrivateKey.
type Signer interface {
	// Sign returns a signature over msg bound to ctx.
	Sign(ctx Context, msg []byte) ([]byte, error)
	// Public returns the verification key.
	Public() PublicKey
}

// PublicKey is an Ed25519 verification key.
type PublicKey struct{ raw ed25519.PublicKey }

// PrivateKey is an Ed25519 signing key and the in-process Signer adapter.
type PrivateKey struct{ raw ed25519.PrivateKey }

// Frame exposes the exact domain-separated bytes that get signed, so remote
// signer adapters (e.g. OpenBao Transit) submit identical input.
func Frame(ctx Context, msg []byte) ([]byte, error) { return frame(ctx, msg) }

// Generate creates a new Ed25519 key pair using the system CSPRNG.
func Generate() (PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PrivateKey{}, fmt.Errorf("signing: generate: %w", err)
	}
	return PrivateKey{raw: priv}, nil
}

// Public returns the verification key.
func (k PrivateKey) Public() PublicKey {
	return PublicKey{raw: k.raw.Public().(ed25519.PublicKey)}
}

// Sign returns a signature over the message bound to ctx.
func (k PrivateKey) Sign(ctx Context, msg []byte) ([]byte, error) {
	m, err := frame(ctx, msg)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(k.raw, m), nil
}

// Verify reports whether sig is valid for msg under ctx. It returns false for
// any malformed input rather than an error, so callers fail closed with a
// single boolean check.
func (p PublicKey) Verify(ctx Context, msg, sig []byte) bool {
	if len(p.raw) != ed25519.PublicKeySize {
		return false
	}
	m, err := frame(ctx, msg)
	if err != nil {
		return false
	}
	return ed25519.Verify(p.raw, m, sig)
}

// Equal reports whether two public keys are identical.
func (p PublicKey) Equal(other PublicKey) bool {
	return p.raw.Equal(other.raw)
}

// String returns the standard base64 encoding of the public key.
func (p PublicKey) String() string {
	return base64.StdEncoding.EncodeToString(p.raw)
}

// MarshalText implements encoding.TextMarshaler for JSON/YAML embedding.
func (p PublicKey) MarshalText() ([]byte, error) {
	return []byte(p.String()), nil
}

// UnmarshalText parses a base64-encoded public key.
func (p *PublicKey) UnmarshalText(text []byte) error {
	b, err := base64.StdEncoding.DecodeString(string(text))
	if err != nil {
		return fmt.Errorf("signing: decode public key: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return fmt.Errorf("signing: bad public key length %d", len(b))
	}
	p.raw = ed25519.PublicKey(b)
	return nil
}

// PublicKeyFromBytes wraps raw Ed25519 public-key bytes.
func PublicKeyFromBytes(b []byte) (PublicKey, error) {
	if len(b) != ed25519.PublicKeySize {
		return PublicKey{}, fmt.Errorf("signing: bad public key length %d", len(b))
	}
	return PublicKey{raw: ed25519.PublicKey(append([]byte(nil), b...))}, nil
}

// Bytes returns the raw public-key bytes.
func (p PublicKey) Bytes() []byte { return append([]byte(nil), p.raw...) }
