package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantmindSSI/haechi/internal/httpapi"
	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
)

func TestKnownEnvironmentsAreAccepted(t *testing.T) {
	for _, env := range []string{"development", "staging", "production"} {
		if err := validateEnvironment(env); err != nil {
			t.Fatalf("%s should be a valid environment: %v", env, err)
		}
	}
	if err := validateEnvironment("prod"); err == nil {
		t.Fatal("an unrecognised environment was accepted; it must not be guessed at")
	}
}

// Durable storage is mandatory outside development. The in-memory store loses
// every tenant, device, policy and enrolment code on restart, so serving real
// traffic from it would silently discard control-plane state.
func TestDurableStorageIsMandatoryOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		t.Setenv("HAECHI_DATABASE_URL", "")
		store, closeStore, err := buildStore(context.Background(), env)
		if err == nil {
			if closeStore != nil {
				closeStore()
			}
			t.Fatalf("%s started with the in-memory store (%T)", env, store)
		}
		if !strings.Contains(err.Error(), "HAECHI_DATABASE_URL") {
			t.Fatalf("%s error should name the missing variable, got: %v", env, err)
		}
	}
}

func TestDevelopmentFallsBackToMemory(t *testing.T) {
	t.Setenv("HAECHI_DATABASE_URL", "")
	store, closeStore, err := buildStore(context.Background(), "development")
	if err != nil {
		t.Fatalf("development should start without a database: %v", err)
	}
	defer closeStore()
	if _, ok := store.(*controlapi.MemoryStore); !ok {
		t.Fatalf("development store is %T, want *controlapi.MemoryStore", store)
	}
}

// An unreachable database must fail at startup rather than on the first
// request, so a broken revision never begins serving.
func TestUnreachableDatabaseFailsAtStartup(t *testing.T) {
	t.Setenv("HAECHI_DATABASE_URL", "postgresql://haechi:haechi@127.0.0.1:1/haechi_control")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	store, closeStore, err := buildStore(ctx, "production")
	if err == nil {
		if closeStore != nil {
			closeStore()
		}
		t.Fatalf("an unreachable database was accepted at startup (%T)", store)
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error should say the database is unreachable, got: %v", err)
	}
}

func TestPoolSizeFallsBackOnInvalidInput(t *testing.T) {
	t.Setenv("HAECHI_DB_MAX_OPEN_CONNS", "not-a-number")
	if got := envInt("HAECHI_DB_MAX_OPEN_CONNS", 10); got != 10 {
		t.Fatalf("invalid pool size returned %d, want the fallback 10", got)
	}
	t.Setenv("HAECHI_DB_MAX_OPEN_CONNS", "-4")
	if got := envInt("HAECHI_DB_MAX_OPEN_CONNS", 10); got != 10 {
		t.Fatalf("negative pool size returned %d, want the fallback 10", got)
	}
	t.Setenv("HAECHI_DB_MAX_OPEN_CONNS", "25")
	if got := envInt("HAECHI_DB_MAX_OPEN_CONNS", 10); got != 25 {
		t.Fatalf("valid pool size returned %d, want 25", got)
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
