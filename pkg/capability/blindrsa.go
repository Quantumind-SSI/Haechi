// Package capability implements Haechi's privacy-preserving access capability
// (Integration Document §6.3, ADR-004): a blindly-signed, audience-bound,
// epoch-limited bearer token that lets a gateway admit an entitled subject
// without learning who they are, and prevents the issuer from linking issuance
// to later presentation.
//
// This file implements the underlying blind-signature primitive: Chaum
// full-domain-hash RSA blind signatures (FDH-RSA), provably one-more-unforgeable
// under the RSA assumption in the random-oracle model, and unconditionally
// blind (the issuer's view is independent of the resulting token).
//
// NOTE (ADR-004 audit gate): production MUST migrate to RFC 9474 (RSABSSA,
// PSS-based) pending independent cryptographic review. FDH-RSA is the
// standardised, auditable Release-1 baseline. It is deliberately implemented
// with the standard library only (crypto/rsa, math/big) to keep the trusted
// computing base small.
//
// # Side-channel posture
//
// math/big is not a constant-time library, and Go's own crypto/rsa does not use
// it for private-key operations: since Go 1.20 that package computes modular
// exponentiation in crypto/internal/bigmod, which is constant time. bigmod is
// internal and cannot be reached from here, so this file must defend itself.
//
// It does so with the two countermeasures that do not require a constant-time
// exponentiation primitive:
//
//   - Base blinding on every secret-exponent operation, so the value fed to the
//     exponentiation is uniformly random and independent of any attacker-chosen
//     input. This is the classic defence against remote chosen-input timing
//     attacks of the Brumley and Boneh kind, and it is why crypto/rsa blinded
//     even before bigmod existed.
//   - A public-exponent verification of every signature before release, which
//     turns a faulted or mistimed private operation into a denial rather than
//     into key-recovering output.
//
// What this does NOT achieve: the windowed Montgomery exponentiation inside
// math/big still performs table lookups indexed by secret exponent bits, so a
// local attacker with cache observation remains in scope. Closing that requires
// a constant-time implementation and is part of the RFC 9474 migration, not of
// this file. Do not describe this construction as side-channel resistant.
package capability

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"io"
	"math/big"
)

var (
	// ErrValueOutOfRange indicates a blinded message or signature was not a
	// residue modulo n. Fail closed.
	ErrValueOutOfRange = errors.New("capability: value out of range mod n")
	// ErrBlindingFailed indicates a blinding factor could not be drawn or
	// inverted within the attempt bound.
	ErrBlindingFailed = errors.New("capability: blinding failed")
	// ErrSigningFault indicates a produced signature did not verify under the
	// issuer public key. The private-key operation was faulted or incorrect, so
	// the result is discarded rather than returned.
	ErrSigningFault = errors.New("capability: signature failed issuer self-check")
)

// maxBlindingAttempts bounds the rejection-sampling loop that draws blinding
// factors. A draw is rejected only when it is zero or shares a factor with the
// modulus, which for an RSA modulus happens with probability around 2^-1000, so
// the loop effectively never repeats. The bound exists so a broken entropy
// source cannot spin forever.
const maxBlindingAttempts = 8

// mgf1 is the PKCS#1 mask-generation function over the given hash.
func mgf1(seed []byte, length int, newHash func() hash.Hash) []byte {
	out := make([]byte, 0, length+sha256.Size)
	var counter uint32
	var c [4]byte
	for len(out) < length {
		h := newHash()
		h.Write(seed)
		binary.BigEndian.PutUint32(c[:], counter)
		h.Write(c[:])
		out = h.Sum(out)
		counter++
	}
	return out[:length]
}

// modulusSize is the fixed octet length of values modulo n.
func modulusSize(n *big.Int) int { return (n.BitLen() + 7) / 8 }

// encodeFixed serialises x as a fixed-length big-endian octet string.
//
// big.Int.Bytes strips leading zeros, so it would emit a shorter slice whenever
// the top octet of a value happens to be zero. That both leaks the magnitude of
// a secret-derived value through its length and diverges from RFC 9474, which
// specifies fixed-length encoding. Values are always reduced modulo n before
// encoding, so they fit.
//
// Preconditions: 0 <= x < 256^size.
func encodeFixed(x *big.Int, size int) []byte {
	out := make([]byte, size)
	x.FillBytes(out)
	return out
}

// fdh maps an arbitrary message to a full-domain element of Z_n. Both signer
// and verifier compute it identically, so the modular reduction is consistent.
func fdh(n *big.Int, msg []byte) *big.Int {
	buf := mgf1(msg, modulusSize(n), sha256.New)
	h := new(big.Int).SetBytes(buf)
	h.Mod(h, n)
	if h.Sign() == 0 {
		h.SetInt64(1)
	}
	return h
}

// blindingPair draws a uniform factor in Z_n^* together with its inverse.
//
// The inverse is not taken directly. big.Int.ModInverse runs the extended
// Euclidean algorithm, whose iteration count depends on its input, so inverting
// the secret factor would leak information about it through timing. Instead the
// factor is masked by a second independent random value before inversion:
//
//	u    = r * t           mod n
//	uInv = u^-1            mod n     (Euclid sees only the uniform u)
//	rInv = uInv * t = r^-1 mod n
//
// u is uniform in Z_n^* and independent of r, so the variable-time step reveals
// nothing about the value that must stay secret. ModInverse returns nil exactly
// when gcd(u, n) != 1, which is true only if r or t shares a factor with n, so
// this also subsumes the coprimality test both factors require.
//
// Complexity: one modular inversion and two multiplications, expected one
// iteration. Bounded by maxBlindingAttempts.
func blindingPair(random io.Reader, n *big.Int) (r, rInv *big.Int, err error) {
	for attempt := 0; attempt < maxBlindingAttempts; attempt++ {
		r, err = rand.Int(random, n)
		if err != nil {
			return nil, nil, err
		}
		t, err := rand.Int(random, n)
		if err != nil {
			return nil, nil, err
		}
		if r.Sign() == 0 || t.Sign() == 0 {
			continue
		}
		u := new(big.Int).Mul(r, t)
		u.Mod(u, n)
		uInv := new(big.Int).ModInverse(u, n)
		if uInv == nil {
			continue
		}
		rInv = uInv.Mul(uInv, t)
		rInv.Mod(rInv, n)
		return r, rInv, nil
	}
	return nil, nil, ErrBlindingFailed
}

// blindState is the client-side secret retained between Blind and Finalize.
//
// It stores the inverse rather than the blinding factor itself, so unblinding
// is a single multiplication and no inversion is ever performed on a secret
// outside blindingPair, where it is masked.
type blindState struct {
	rInv *big.Int // inverse of the blinding factor; kept secret, never sent
}

// blind computes a blinded representative of msg under the issuer public key.
// The issuer that signs the returned bytes learns nothing about msg or the
// resulting signature: this is what makes the capability unlinkable.
func blind(pub *rsa.PublicKey, msg []byte) (blinded []byte, st *blindState, err error) {
	h := fdh(pub.N, msg)
	r, rInv, err := blindingPair(rand.Reader, pub.N)
	if err != nil {
		return nil, nil, err
	}
	e := big.NewInt(int64(pub.E))
	rE := new(big.Int).Exp(r, e, pub.N)
	b := new(big.Int).Mul(h, rE)
	b.Mod(b, pub.N)
	return encodeFixed(b, modulusSize(pub.N)), &blindState{rInv: rInv}, nil
}

// blindSign applies the RSA private exponent to a blinded message. The issuer
// calls this; it never sees the unblinded token.
//
// The input is fully attacker-chosen: a client may submit any residue and time
// the response, so the signer must not assume the value was honestly blinded.
// A fresh signer-side blind is therefore applied on every call, and the result
// is checked against the public exponent before release. See the side-channel
// note in the package comment for what this does and does not cover.
//
// The private exponent is applied directly rather than through the Chinese
// remainder theorem. CRT would be about four times faster, but a fault in
// either half leaks a factor of the modulus, and capability issuance is not on
// a throughput-critical path.
//
// Complexity: three modular exponentiations, two of them with the small public
// exponent.
func blindSign(priv *rsa.PrivateKey, blinded []byte) ([]byte, error) {
	b := new(big.Int).SetBytes(blinded)
	if b.Cmp(priv.N) >= 0 || b.Sign() == 0 {
		return nil, ErrValueOutOfRange
	}
	v, vInv, err := blindingPair(rand.Reader, priv.N)
	if err != nil {
		return nil, err
	}
	e := big.NewInt(int64(priv.E))

	// masked = b * v^e, so the exponentiation below consumes a uniformly
	// random base whatever the caller supplied.
	masked := new(big.Int).Exp(v, e, priv.N)
	masked.Mul(masked, b)
	masked.Mod(masked, priv.N)

	// (b * v^e)^d = b^d * v, so multiplying by v^-1 recovers b^d.
	s := new(big.Int).Exp(masked, priv.D, priv.N)
	s.Mul(s, vInv)
	s.Mod(s, priv.N)

	// Fault check: a correct signature satisfies s^e == b (mod n). Releasing a
	// faulted signature can disclose the private key, so deny instead.
	if new(big.Int).Exp(s, e, priv.N).Cmp(b) != 0 {
		return nil, ErrSigningFault
	}
	return encodeFixed(s, modulusSize(priv.N)), nil
}

// finalize removes the blinding factor, yielding a standard FDH-RSA signature
// over the original message that verifies under the issuer public key.
//
// This is a single modular multiplication. The inversion it would otherwise
// need was performed, masked, in blindingPair.
func finalize(pub *rsa.PublicKey, st *blindState, blindSig []byte) ([]byte, error) {
	if st == nil || st.rInv == nil {
		return nil, ErrBlindingFailed
	}
	s2 := new(big.Int).SetBytes(blindSig)
	if s2.Cmp(pub.N) >= 0 {
		return nil, ErrValueOutOfRange
	}
	s := s2.Mul(s2, st.rInv)
	s.Mod(s, pub.N)
	return encodeFixed(s, modulusSize(pub.N)), nil
}

// verifyBlindRSA reports whether sig is a valid FDH-RSA signature over msg.
// Only public values are involved, so no blinding is required.
func verifyBlindRSA(pub *rsa.PublicKey, msg, sig []byte) bool {
	s := new(big.Int).SetBytes(sig)
	if s.Cmp(pub.N) >= 0 {
		return false
	}
	e := big.NewInt(int64(pub.E))
	m := new(big.Int).Exp(s, e, pub.N)
	return m.Cmp(fdh(pub.N, msg)) == 0
}
