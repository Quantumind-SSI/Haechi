package capability

import (
	"bytes"
	"context"
	"testing"
)

// testBits keeps unit tests fast while remaining a real RSA modulus. Production
// uses >= 2048 (enforced by NewIssuer default and policy).
const testBits = 2048

// mint runs the full client<->issuer flow and returns a presentable token.
func mint(t *testing.T, is *Issuer, attrs Attributes) *Token {
	t.Helper()
	keyID, pub, err := is.PublicKey(attrs)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	bl, err := Prepare(keyID, pub, attrs)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	kid, blindSig, err := is.BlindSign(attrs, bl.Blinded)
	if err != nil {
		t.Fatalf("blind sign: %v", err)
	}
	if kid != keyID {
		t.Fatalf("key id mismatch: %s != %s", kid, keyID)
	}
	tok, err := bl.Finalize(blindSig)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return tok
}

func verifierFor(is *Issuer, audience string, classes []string, epochs []uint64) *Verifier {
	var keys []VerifierKey
	for _, d := range is.Directory() {
		keys = append(keys, VerifierKey{Pub: d.Pub, Attrs: d.Attrs})
	}
	return NewVerifier(audience, classes, epochs, keys, NewMemoryReplayCache())
}

func TestHappyPathAdmission(t *testing.T) {
	is := NewIssuer(testBits)
	attrs := Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42}
	tok := mint(t, is, attrs)

	v := verifierFor(is, "eu-standard", []string{"general"}, []uint64{42})
	got, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("valid capability denied: %v", err)
	}
	if got != attrs {
		t.Fatalf("attributes mismatch: %+v", got)
	}
}

func TestWrongAudienceDenied(t *testing.T) {
	is := NewIssuer(testBits)
	tok := mint(t, is, Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42})

	// Present an EU token to a US gateway. The US verifier does not trust the
	// EU key at all -> unknown key. Even if it did, audience would mismatch.
	v := verifierFor(is, "us-standard", []string{"general"}, []uint64{42})
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("token accepted at wrong-audience gateway")
	}
}

func TestExpiredEpochDenied(t *testing.T) {
	is := NewIssuer(testBits)
	tok := mint(t, is, Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 41})

	v := verifierFor(is, "eu-standard", []string{"general"}, []uint64{42, 43}) // 41 no longer valid
	if _, err := v.Verify(context.Background(), tok); err != ErrEpochExpired && err != ErrUnknownKey {
		t.Fatalf("expired epoch not denied: %v", err)
	}
}

func TestServiceClassNotAllowedDenied(t *testing.T) {
	is := NewIssuer(testBits)
	tok := mint(t, is, Attributes{Audience: "eu-standard", ServiceClass: "premium", Epoch: 42})

	// Gateway serves only "general".
	v := NewVerifier("eu-standard", []string{"general"}, []uint64{42},
		directoryKeys(is), NewMemoryReplayCache())
	if _, err := v.Verify(context.Background(), tok); err != ErrServiceClassNotAllowed {
		t.Fatalf("want ErrServiceClassNotAllowed, got %v", err)
	}
}

func TestTamperedSignatureDenied(t *testing.T) {
	is := NewIssuer(testBits)
	tok := mint(t, is, Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42})
	tok.Sig[0] ^= 0xFF // flip a bit

	v := verifierFor(is, "eu-standard", []string{"general"}, []uint64{42})
	if _, err := v.Verify(context.Background(), tok); err != ErrBadSignature {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
}

func TestTamperedNonceDenied(t *testing.T) {
	is := NewIssuer(testBits)
	tok := mint(t, is, Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42})
	tok.Nonce[0] ^= 0x01

	v := verifierFor(is, "eu-standard", []string{"general"}, []uint64{42})
	if _, err := v.Verify(context.Background(), tok); err != ErrBadSignature {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
}

func TestReplayDenied(t *testing.T) {
	is := NewIssuer(testBits)
	tok := mint(t, is, Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42})

	v := verifierFor(is, "eu-standard", []string{"general"}, []uint64{42})
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("first use denied: %v", err)
	}
	if _, err := v.Verify(context.Background(), tok); err != ErrReplay {
		t.Fatalf("want ErrReplay on second use, got %v", err)
	}
}

func TestReplayCacheFailsClosedAtCapacity(t *testing.T) {
	cache := &MemoryReplayCache{seen: map[uint64]map[string]struct{}{}, max: 1}
	if err := cache.Spend(context.Background(), 42, []byte("first")); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := cache.Spend(context.Background(), 42, []byte("second")); err != ErrReplayCacheFull {
		t.Fatalf("want ErrReplayCacheFull, got %v", err)
	}
	if err := cache.Spend(context.Background(), 42, []byte("first")); err != ErrReplay {
		t.Fatalf("want ErrReplay, got %v", err)
	}
}

func TestUnknownKeyDenied(t *testing.T) {
	is := NewIssuer(testBits)
	tok := mint(t, is, Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42})
	tok.KeyID = "not-a-real-key-id"

	v := verifierFor(is, "eu-standard", []string{"general"}, []uint64{42})
	if _, err := v.Verify(context.Background(), tok); err != ErrUnknownKey {
		t.Fatalf("want ErrUnknownKey, got %v", err)
	}
}

func TestAttributeEncodingIsUnambiguous(t *testing.T) {
	left := Attributes{Audience: "eu|standard", ServiceClass: "general", Epoch: 42}
	right := Attributes{Audience: "eu", ServiceClass: "standard|general", Epoch: 42}
	if left.canonical() == right.canonical() {
		t.Fatal("distinct attribute tuples produced the same canonical value")
	}
}

// TestIssuerBlindness proves the issuer's view (the blinded message) is
// independent of the resulting token nonce: the crux of unlinkability.
func TestIssuerBlindness(t *testing.T) {
	is := NewIssuer(testBits)
	attrs := Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42}
	keyID, pub, _ := is.PublicKey(attrs)

	bl, _ := Prepare(keyID, pub, attrs)
	// The blinded value the issuer sees must not equal or contain the nonce.
	if bytes.Contains(bl.Blinded, bl.nonce) {
		t.Fatal("blinded message leaks the nonce to the issuer")
	}
	_, blindSig, _ := is.BlindSign(attrs, bl.Blinded)
	tok, _ := bl.Finalize(blindSig)
	if bytes.Equal(bl.Blinded, tok.Nonce) {
		t.Fatal("issuer view equals token nonce")
	}
}

// TestUnlinkabilityDistinctTokens proves two issuances for the same attributes
// yield unlinkable tokens (distinct nonces, both valid).
func TestUnlinkabilityDistinctTokens(t *testing.T) {
	is := NewIssuer(testBits)
	attrs := Attributes{Audience: "eu-standard", ServiceClass: "general", Epoch: 42}
	t1 := mint(t, is, attrs)
	t2 := mint(t, is, attrs)
	if bytes.Equal(t1.Nonce, t2.Nonce) {
		t.Fatal("two issuances produced identical nonces")
	}
	v := verifierFor(is, "eu-standard", []string{"general"}, []uint64{42})
	if _, err := v.Verify(context.Background(), t1); err != nil {
		t.Fatalf("t1 denied: %v", err)
	}
	if _, err := v.Verify(context.Background(), t2); err != nil {
		t.Fatalf("t2 denied: %v", err)
	}
}

func directoryKeys(is *Issuer) []VerifierKey {
	var keys []VerifierKey
	for _, d := range is.Directory() {
		keys = append(keys, VerifierKey{Pub: d.Pub, Attrs: d.Attrs})
	}
	return keys
}
