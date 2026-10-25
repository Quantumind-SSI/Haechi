package capability

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"math/big"
	"sync"
	"testing"
)

// sharedKey is generated once. RSA key generation dominates the runtime of this
// file, and none of these tests mutate the key.
var (
	sharedKeyOnce sync.Once
	sharedKey     *rsa.PrivateKey
)

func testKey(t testing.TB) *rsa.PrivateKey {
	t.Helper()
	sharedKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		sharedKey = k
	})
	return sharedKey
}

// zeroReader yields an endless run of zero bytes, which makes rand.Int return
// zero and drives blindingPair into its rejection path every time.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// deadReader fails on first use, standing in for an exhausted entropy source.
type deadReader struct{}

var errNoEntropy = errors.New("entropy unavailable")

func (deadReader) Read([]byte) (int, error) { return 0, errNoEntropy }

func TestBlindingPairProducesATrueInverse(t *testing.T) {
	n := testKey(t).N
	for i := 0; i < 16; i++ {
		r, rInv, err := blindingPair(rand.Reader, n)
		if err != nil {
			t.Fatalf("blindingPair: %v", err)
		}
		product := new(big.Int).Mul(r, rInv)
		product.Mod(product, n)
		if product.Cmp(big.NewInt(1)) != 0 {
			t.Fatalf("r * rInv = %s mod n, want 1", product)
		}
		if r.Sign() == 0 || r.Cmp(n) >= 0 {
			t.Fatalf("blinding factor %s outside [1, n)", r)
		}
	}
}

func TestBlindingPairDrawsFreshFactors(t *testing.T) {
	n := testKey(t).N
	first, _, err := blindingPair(rand.Reader, n)
	if err != nil {
		t.Fatalf("blindingPair: %v", err)
	}
	second, _, err := blindingPair(rand.Reader, n)
	if err != nil {
		t.Fatalf("blindingPair: %v", err)
	}
	if first.Cmp(second) == 0 {
		t.Fatal("two draws produced the same blinding factor")
	}
}

func TestBlindingPairPropagatesEntropyFailure(t *testing.T) {
	if _, _, err := blindingPair(deadReader{}, testKey(t).N); !errors.Is(err, errNoEntropy) {
		t.Fatalf("got %v, want the underlying entropy error", err)
	}
}

func TestBlindingPairGivesUpRatherThanSpinning(t *testing.T) {
	_, _, err := blindingPair(zeroReader{}, testKey(t).N)
	if !errors.Is(err, ErrBlindingFailed) {
		t.Fatalf("got %v, want ErrBlindingFailed after the attempt bound", err)
	}
}

func TestEncodeFixedPadsRatherThanTruncating(t *testing.T) {
	// big.Int.Bytes would return a single byte here; the encoding must not.
	got := encodeFixed(big.NewInt(1), 8)
	want := []byte{0, 0, 0, 0, 0, 0, 0, 1}
	if !bytes.Equal(got, want) {
		t.Fatalf("encodeFixed(1, 8) = %v, want %v", got, want)
	}
	if len(encodeFixed(big.NewInt(0), 4)) != 4 {
		t.Fatal("zero must still occupy the full width")
	}
}

func TestBlindedValuesAreAlwaysModulusWidth(t *testing.T) {
	priv := testKey(t)
	width := modulusSize(priv.N)
	for i := 0; i < 8; i++ {
		blinded, st, err := blind(&priv.PublicKey, []byte("nonce"))
		if err != nil {
			t.Fatalf("blind: %v", err)
		}
		if len(blinded) != width {
			t.Fatalf("blinded value is %d bytes, want %d", len(blinded), width)
		}
		blindSig, err := blindSign(priv, blinded)
		if err != nil {
			t.Fatalf("blindSign: %v", err)
		}
		if len(blindSig) != width {
			t.Fatalf("blind signature is %d bytes, want %d", len(blindSig), width)
		}
		sig, err := finalize(&priv.PublicKey, st, blindSig)
		if err != nil {
			t.Fatalf("finalize: %v", err)
		}
		if len(sig) != width {
			t.Fatalf("signature is %d bytes, want %d", len(sig), width)
		}
	}
}

// TestSignerBlindingDoesNotChangeTheSignature is the correctness proof for the
// signer-side countermeasure. b^d mod n is a unique value, so if the fresh
// per-call blind were not removed exactly, repeated signatures would differ.
func TestSignerBlindingDoesNotChangeTheSignature(t *testing.T) {
	priv := testKey(t)
	blinded, _, err := blind(&priv.PublicKey, []byte("nonce"))
	if err != nil {
		t.Fatalf("blind: %v", err)
	}
	first, err := blindSign(priv, blinded)
	if err != nil {
		t.Fatalf("blindSign: %v", err)
	}
	for i := 0; i < 8; i++ {
		again, err := blindSign(priv, blinded)
		if err != nil {
			t.Fatalf("blindSign: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("signer blinding was not removed exactly; signatures differ")
		}
	}
}

func TestBlindSignRejectsValuesOutsideTheGroup(t *testing.T) {
	priv := testKey(t)
	width := modulusSize(priv.N)
	cases := map[string]*big.Int{
		"zero":             big.NewInt(0),
		"equal to modulus": new(big.Int).Set(priv.N),
		"above modulus":    new(big.Int).Add(priv.N, big.NewInt(1)),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			buf := make([]byte, width+1)
			value.FillBytes(buf)
			if _, err := blindSign(priv, buf); !errors.Is(err, ErrValueOutOfRange) {
				t.Fatalf("got %v, want ErrValueOutOfRange", err)
			}
		})
	}
}

// TestBlindSignRefusesToReleaseAFaultedSignature corrupts the private exponent,
// which is what a fault injection would achieve. The self-check must deny
// rather than emit a signature, because a faulted RSA signature can disclose
// the key.
func TestBlindSignRefusesToReleaseAFaultedSignature(t *testing.T) {
	priv := testKey(t)
	faulted := *priv
	faulted.D = new(big.Int).Add(priv.D, big.NewInt(1))

	blinded, _, err := blind(&priv.PublicKey, []byte("nonce"))
	if err != nil {
		t.Fatalf("blind: %v", err)
	}
	sig, err := blindSign(&faulted, blinded)
	if !errors.Is(err, ErrSigningFault) {
		t.Fatalf("got (%v, %v), want ErrSigningFault", sig, err)
	}
	if sig != nil {
		t.Fatal("a faulted signature must not be returned alongside the error")
	}
}

func TestFinalizeFailsClosedOnBadInput(t *testing.T) {
	priv := testKey(t)
	pub := &priv.PublicKey

	if _, err := finalize(pub, nil, []byte{1}); !errors.Is(err, ErrBlindingFailed) {
		t.Fatalf("nil state: got %v, want ErrBlindingFailed", err)
	}
	if _, err := finalize(pub, &blindState{}, []byte{1}); !errors.Is(err, ErrBlindingFailed) {
		t.Fatalf("empty state: got %v, want ErrBlindingFailed", err)
	}
	oversized := make([]byte, modulusSize(pub.N)+1)
	new(big.Int).Add(pub.N, big.NewInt(1)).FillBytes(oversized)
	_, st, err := blind(pub, []byte("nonce"))
	if err != nil {
		t.Fatalf("blind: %v", err)
	}
	if _, err := finalize(pub, st, oversized); !errors.Is(err, ErrValueOutOfRange) {
		t.Fatalf("oversized signature: got %v, want ErrValueOutOfRange", err)
	}
}

// TestRoundTripSurvivesRepetition exercises the whole primitive often enough to
// catch the leading-zero encoding cases that occur about once in 256 draws.
func TestRoundTripSurvivesRepetition(t *testing.T) {
	priv := testKey(t)
	pub := &priv.PublicKey
	for i := 0; i < 64; i++ {
		msg := make([]byte, 32)
		if _, err := rand.Read(msg); err != nil {
			t.Fatalf("read nonce: %v", err)
		}
		blinded, st, err := blind(pub, msg)
		if err != nil {
			t.Fatalf("blind: %v", err)
		}
		blindSig, err := blindSign(priv, blinded)
		if err != nil {
			t.Fatalf("blindSign: %v", err)
		}
		sig, err := finalize(pub, st, blindSig)
		if err != nil {
			t.Fatalf("finalize: %v", err)
		}
		if !verifyBlindRSA(pub, msg, sig) {
			t.Fatalf("round trip %d produced a signature that does not verify", i)
		}
		if verifyBlindRSA(pub, []byte("a different message"), sig) {
			t.Fatalf("round trip %d verified under the wrong message", i)
		}
	}
}

// TestIssuerLearnsNothingFromTheBlindedValue restates the unlinkability
// property at the primitive level: the value the issuer signs must be
// independent of the message, so two blinds of the same message differ.
func TestIssuerLearnsNothingFromTheBlindedValue(t *testing.T) {
	priv := testKey(t)
	msg := []byte("the same nonce twice")
	first, _, err := blind(&priv.PublicKey, msg)
	if err != nil {
		t.Fatalf("blind: %v", err)
	}
	second, _, err := blind(&priv.PublicKey, msg)
	if err != nil {
		t.Fatalf("blind: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("blinding is not randomised; the issuer could link two requests")
	}
	if bytes.Contains(first, msg) || bytes.Contains(second, msg) {
		t.Fatal("the blinded value leaks the message")
	}
}

// BenchmarkBlindSign measures the issuer-side operation. It performs three
// modular exponentiations rather than one: the private-exponent operation plus
// two cheap public-exponent ones for the signer blind and the fault check. The
// public-exponent operations are close to free at e = 65537, so the added cost
// is small next to the private operation, and issuance is not on a
// throughput-critical path.
func BenchmarkBlindSign(b *testing.B) {
	priv := testKey(b)
	blinded, _, err := blind(&priv.PublicKey, []byte("nonce"))
	if err != nil {
		b.Fatalf("blind: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := blindSign(priv, blinded); err != nil {
			b.Fatalf("blindSign: %v", err)
		}
	}
}

// BenchmarkBlind measures the client-side blind, which now also computes the
// masked inverse that Finalize consumes.
func BenchmarkBlind(b *testing.B) {
	priv := testKey(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := blind(&priv.PublicKey, []byte("nonce")); err != nil {
			b.Fatalf("blind: %v", err)
		}
	}
}

// BenchmarkFinalize measures unblinding, now a single modular multiplication
// because the inversion moved into blind.
func BenchmarkFinalize(b *testing.B) {
	priv := testKey(b)
	blinded, st, err := blind(&priv.PublicKey, []byte("nonce"))
	if err != nil {
		b.Fatalf("blind: %v", err)
	}
	blindSig, err := blindSign(priv, blinded)
	if err != nil {
		b.Fatalf("blindSign: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := finalize(&priv.PublicKey, st, blindSig); err != nil {
			b.Fatalf("finalize: %v", err)
		}
	}
}
