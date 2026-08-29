package main

import (
	"context"
	"testing"

	"github.com/QuantmindSSI/haechi/internal/httpapi"
)

func TestProductionEnvironmentIsBlockedWithoutDurableStore(t *testing.T) {
	if err := validateEnvironment("production"); err == nil {
		t.Fatal("production environment was accepted without durable storage")
	}
}

func TestDevelopmentAuthenticationRequiresStrongExplicitToken(t *testing.T) {
	t.Setenv("KEYCLOAK_ISSUER", "")
	t.Setenv("KEYCLOAK_AUDIENCE", "")
	t.Setenv("HAECHI_DEV_ADMIN_TOKEN", "short")
	if _, err := buildIdentityVerifier("development"); err == nil {
		t.Fatal("weak development token was accepted")
	}

	token := "0123456789abcdef0123456789abcdef"
	t.Setenv("HAECHI_DEV_ADMIN_TOKEN", token)
	verifier, err := buildIdentityVerifier("development")
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	identity, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("verify development token: %v", err)
	}
	if len(identity.Entitlements) != 1 || identity.Entitlements[0] != httpapi.ControlAdminEntitlement {
		t.Fatalf("unexpected entitlements: %v", identity.Entitlements)
	}
}

func TestKeycloakConfigurationMustBeComplete(t *testing.T) {
	t.Setenv("KEYCLOAK_ISSUER", "https://identity.example")
	t.Setenv("KEYCLOAK_AUDIENCE", "")
	t.Setenv("HAECHI_DEV_ADMIN_TOKEN", "0123456789abcdef0123456789abcdef")
	if _, err := buildIdentityVerifier("development"); err == nil {
		t.Fatal("partial Keycloak configuration was accepted")
	}
}
