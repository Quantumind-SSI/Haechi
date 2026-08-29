package capissuer

import (
	"context"
	"testing"

	"github.com/QuantmindSSI/haechi/internal/signing"
	"github.com/QuantmindSSI/haechi/pkg/capability"
	"github.com/QuantmindSSI/haechi/services/entitlement"
)

func TestPublicKeyRequiresMatchingSignedGrant(t *testing.T) {
	entitlementKey, err := signing.Generate()
	if err != nil {
		t.Fatalf("entitlement key: %v", err)
	}
	identityVerifier := entitlement.NewStaticVerifier()
	identityVerifier.Add("identity-token", entitlement.Identity{
		TenantID: "tenant", Subject: "subject", Entitlements: []string{"eu-standard|general"},
	})
	entitlementService := entitlement.NewService(entitlementKey, "entitlement-key", identityVerifier)
	grant, err := entitlementService.Issue(context.Background(), "identity-token", "eu-standard", "general")
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	service := NewService(entitlementService.Pub(), 2048)
	attrs := capability.Attributes{
		Audience: "eu-standard", ServiceClass: "general", Epoch: grant.Grant.Epoch,
	}
	if _, _, err := service.PublicKey(grant, attrs); err != nil {
		t.Fatalf("authorized public key lookup: %v", err)
	}
	if _, _, err := service.PublicKey(entitlement.SignedGrant{}, attrs); err != ErrBadGrant {
		t.Fatalf("unsigned grant error = %v, want ErrBadGrant", err)
	}
	mismatched := attrs
	mismatched.ServiceClass = "premium"
	if _, _, err := service.PublicKey(grant, mismatched); err != ErrAttrMismatch {
		t.Fatalf("mismatched attributes error = %v, want ErrAttrMismatch", err)
	}
	if len(service.Directory()) != 1 {
		t.Fatalf("unauthorized lookups created keys: %d", len(service.Directory()))
	}
}
