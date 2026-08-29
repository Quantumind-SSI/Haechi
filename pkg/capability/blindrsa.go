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
package capability

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"math/big"
)

var (
	// ErrValueOutOfRange indicates a blinded message or signature was not a
	// residue modulo n. Fail closed.
	ErrValueOutOfRange = errors.New("capability: value out of range mod n")
	// ErrBlindingFailed indicates the blinding factor could not be inverted.
	ErrBlindingFailed = errors.New("capability: blinding failed")
)

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

// fdh maps an arbitrary message to a full-domain element of Z_n. Both signer
// and verifier compute it identically, so the modular reduction is consistent.
func fdh(n *big.Int, msg []byte) *big.Int {
	k := (n.BitLen() + 7) / 8
	buf := mgf1(msg, k, sha256.New)
	h := new(big.Int).SetBytes(buf)
	h.Mod(h, n)
	if h.Sign() == 0 {
		h.SetInt64(1)
	}
	return h
}

// blindState is the client-side secret retained between Blind and Finalize.
type blindState struct {
	r *big.Int // blinding factor; kept secret, never sent to the issuer
}

// blind computes a blinded representative of msg under the issuer public key.
// The issuer that signs the returned bytes learns nothing about msg or the
// resulting signature: this is what makes the capability unlinkable.
func blind(pub *rsa.PublicKey, msg []byte) (blinded []byte, st *blindState, err error) {
	h := fdh(pub.N, msg)
	r, err := randCoprime(pub.N)
	if err != nil {
		return nil, nil, err
	}
	e := big.NewInt(int64(pub.E))
	rE := new(big.Int).Exp(r, e, pub.N)
	b := new(big.Int).Mul(h, rE)
	b.Mod(b, pub.N)
	return b.Bytes(), &blindState{r: r}, nil
}

// blindSign applies the RSA private exponent to a blinded message. The issuer
// calls this; it never sees the unblinded token.
func blindSign(priv *rsa.PrivateKey, blinded []byte) ([]byte, error) {
	b := new(big.Int).SetBytes(blinded)
	if b.Cmp(priv.N) >= 0 || b.Sign() == 0 {
		return nil, ErrValueOutOfRange
	}
	s := new(big.Int).Exp(b, priv.D, priv.N)
	return s.Bytes(), nil
}

// finalize removes the blinding factor, yielding a standard FDH-RSA signature
// over the original message that verifies under the issuer public key.
func finalize(pub *rsa.PublicKey, st *blindState, blindSig []byte) ([]byte, error) {
	s2 := new(big.Int).SetBytes(blindSig)
	if s2.Cmp(pub.N) >= 0 {
		return nil, ErrValueOutOfRange
	}
	rInv := new(big.Int).ModInverse(st.r, pub.N)
	if rInv == nil {
		return nil, ErrBlindingFailed
	}
	s := new(big.Int).Mul(s2, rInv)
	s.Mod(s, pub.N)
	return s.Bytes(), nil
}

// verifyBlindRSA reports whether sig is a valid FDH-RSA signature over msg.
func verifyBlindRSA(pub *rsa.PublicKey, msg, sig []byte) bool {
	s := new(big.Int).SetBytes(sig)
	if s.Cmp(pub.N) >= 0 {
		return false
	}
	e := big.NewInt(int64(pub.E))
	m := new(big.Int).Exp(s, e, pub.N)
	return m.Cmp(fdh(pub.N, msg)) == 0
}

// randCoprime returns a uniform r in [1,n) with gcd(r,n)=1.
func randCoprime(n *big.Int) (*big.Int, error) {
	one := big.NewInt(1)
	for i := 0; i < 1024; i++ {
		r, err := rand.Int(rand.Reader, n)
		if err != nil {
			return nil, err
		}
		if r.Sign() == 0 {
			continue
		}
		if new(big.Int).GCD(nil, nil, r, n).Cmp(one) == 0 {
			return r, nil
		}
	}
	return nil, ErrBlindingFailed
}
