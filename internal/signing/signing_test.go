package signing

import "testing"

func TestSignVerifyRoundTrip(t *testing.T) {
	k, err := Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	msg := []byte("route-plan-bytes")
	sig, err := k.Sign(ContextRoutePlan, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !k.Public().Verify(ContextRoutePlan, msg, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestDomainSeparationPreventsCrossContextReplay(t *testing.T) {
	k, _ := Generate()
	msg := []byte("same-bytes")
	sig, _ := k.Sign(ContextRoutePlan, msg)

	// A route-plan signature must NOT verify as a policy-bundle signature,
	// even over identical bytes. This is the core fail-closed property.
	if k.Public().Verify(ContextPolicyBundle, msg, sig) {
		t.Fatal("cross-context signature accepted: domain separation broken")
	}
}

func TestTamperedMessageRejected(t *testing.T) {
	k, _ := Generate()
	sig, _ := k.Sign(ContextGatewayIdentity, []byte("gw-1"))
	if k.Public().Verify(ContextGatewayIdentity, []byte("gw-2"), sig) {
		t.Fatal("tampered message accepted")
	}
}

func TestEmptyContextFailsClosed(t *testing.T) {
	k, _ := Generate()
	if _, err := k.Sign("", []byte("x")); err != ErrEmptyContext {
		t.Fatalf("want ErrEmptyContext, got %v", err)
	}
	if k.Public().Verify("", []byte("x"), []byte("y")) {
		t.Fatal("verify with empty context must fail closed")
	}
}

func TestPublicKeyTextRoundTrip(t *testing.T) {
	k, _ := Generate()
	pub := k.Public()
	txt, err := pub.MarshalText()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PublicKey
	if err := got.UnmarshalText(txt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Equal(pub) {
		t.Fatal("public key did not round-trip")
	}
}

func TestVerifyWrongKeyRejected(t *testing.T) {
	k1, _ := Generate()
	k2, _ := Generate()
	sig, _ := k1.Sign(ContextPolicyBundle, []byte("bundle"))
	if k2.Public().Verify(ContextPolicyBundle, []byte("bundle"), sig) {
		t.Fatal("signature verified under wrong key")
	}
}
