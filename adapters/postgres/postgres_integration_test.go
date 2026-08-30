// Integration tests for the PostgreSQL store.
//
// These run only when HAECHI_CONTROL_TEST_DATABASE_URL points at a disposable
// PostgreSQL database. The name is distinct from the website suite's variable
// because the two use different schemas in different databases, so the suite still runs offline. CI supplies it from a
// service container.
//
//	initdb -D /tmp/haechi-pg -U postgres --auth=trust
//	pg_ctl -D /tmp/haechi-pg -o "-p 55432" -l /tmp/haechi-pg/server.log start
//	createdb -h 127.0.0.1 -p 55432 -U postgres haechi_control
//	export HAECHI_CONTROL_TEST_DATABASE_URL="postgresql://postgres@127.0.0.1:55432/haechi_control"
//
// The behaviour proven here cannot be proven against the memory store: tenant
// isolation in SQL, transactional enrolment under real concurrency, and
// context cancellation reaching the driver.
package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/QuantmindSSI/haechi/adapters/postgres"
	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
)

func newStore(t *testing.T) (*postgres.Store, *sql.DB) {
	t.Helper()
	url := os.Getenv("HAECHI_CONTROL_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HAECHI_CONTROL_TEST_DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	schema, err := os.ReadFile(filepath.Join("..", "..", "deploy", "postgres", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	// Order matters: children reference tenants.
	for _, table := range []string{"gateways", "devices", "enrollment_codes", "tenant_policies", "tenants"} {
		if _, err := db.ExecContext(context.Background(), "DELETE FROM "+table); err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	return postgres.New(db), db
}

func tenant(id string) model.Tenant {
	return model.Tenant{ID: id, Name: id, CreatedAt: time.Now().UTC().Truncate(time.Second)}
}

func defaultPolicy() controlapi.TenantPolicy {
	return controlapi.TenantPolicy{
		AllowedServiceClasses: []string{},
		AllowedTransports:     []model.Transport{model.TransportDirect},
		PQRequirement:         model.PQPreferred,
		KillSwitch:            true,
		DNS:                   []string{},
	}
}

func TestPingReportsReachability(t *testing.T) {
	store, _ := newStore(t)
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestTenantCreationIsAtomicWithItsPolicy(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	if err := store.CreateTenant(ctx, tenant("ten_a"), defaultPolicy()); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	policy, err := store.GetTenantPolicy(ctx, "ten_a")
	if err != nil {
		t.Fatalf("policy should exist alongside the tenant: %v", err)
	}
	if !policy.KillSwitch || policy.Version != 1 {
		t.Fatalf("unexpected initial policy: %+v", policy)
	}

	// A duplicate must not leave a partial write behind.
	if err := store.CreateTenant(ctx, tenant("ten_a"), defaultPolicy()); err == nil {
		t.Fatal("duplicate tenant was accepted")
	}
}

func TestGatewaysAreIsolatedByTenant(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	for _, id := range []string{"ten_a", "ten_b"} {
		if err := store.CreateTenant(ctx, tenant(id), defaultPolicy()); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	for _, g := range []model.Gateway{
		{ID: "gw-a1", TenantID: "ten_a", Audience: "eu", ServiceClasses: []string{"general"}},
		{ID: "gw-a2", TenantID: "ten_a", Audience: "eu", ServiceClasses: []string{"general"}},
		{ID: "gw-b1", TenantID: "ten_b", Audience: "eu", ServiceClasses: []string{"general"}},
	} {
		if err := store.UpsertGateway(ctx, g); err != nil {
			t.Fatalf("upsert %s: %v", g.ID, err)
		}
	}

	a, err := store.ListGateways(ctx, "ten_a")
	if err != nil {
		t.Fatalf("list ten_a: %v", err)
	}
	if len(a) != 2 {
		t.Fatalf("ten_a sees %d gateways, want 2", len(a))
	}
	for _, g := range a {
		if g.TenantID != "ten_a" {
			t.Fatalf("ten_a query returned a gateway owned by %s", g.TenantID)
		}
	}

	b, err := store.ListGateways(ctx, "ten_b")
	if err != nil {
		t.Fatalf("list ten_b: %v", err)
	}
	if len(b) != 1 || b[0].ID != "gw-b1" {
		t.Fatalf("ten_b sees %+v, want only gw-b1", b)
	}

	// A gateway id belonging to another tenant must not be stealable.
	err = store.UpsertGateway(ctx, model.Gateway{ID: "gw-a1", TenantID: "ten_b", Audience: "eu"})
	if !errors.Is(err, controlapi.ErrConflict) {
		t.Fatalf("cross-tenant gateway takeover returned %v, want ErrConflict", err)
	}
}

func TestEnrollmentCodeIsConsumedExactlyOnceUnderConcurrency(t *testing.T) {
	store, db := newStore(t)
	ctx := context.Background()

	if err := store.CreateTenant(ctx, tenant("ten_a"), defaultPolicy()); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	code := controlapi.EnrollmentCode{
		Code:      "code-key-1",
		TenantID:  "ten_a",
		Challenge: []byte("challenge"),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.PutEnrollmentCode(ctx, code); err != nil {
		t.Fatalf("put code: %v", err)
	}

	key, err := signing.Generate()
	if err != nil {
		t.Fatalf("device key: %v", err)
	}

	const attempts = 12
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			device := model.Device{
				ID:         "dev_" + string(rune('a'+n)),
				TenantID:   "ten_a",
				PublicKey:  key.Public(),
				Platform:   "linux",
				EnrolledAt: time.Now(),
			}
			results <- store.ConsumeEnrollmentCode(ctx, code.Code, time.Now(), device)
		}(i)
	}
	wg.Wait()
	close(results)

	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
			continue
		}
		if !errors.Is(err, controlapi.ErrCodeUsed) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d concurrent enrolments succeeded, want exactly 1", succeeded)
	}

	var devices int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM devices").Scan(&devices); err != nil {
		t.Fatalf("count devices: %v", err)
	}
	if devices != 1 {
		t.Fatalf("%d devices persisted, want 1", devices)
	}
}

func TestExpiredCodeIsRefused(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	if err := store.CreateTenant(ctx, tenant("ten_a"), defaultPolicy()); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := store.PutEnrollmentCode(ctx, controlapi.EnrollmentCode{
		Code: "stale", TenantID: "ten_a", Challenge: []byte("c"),
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("put code: %v", err)
	}
	key, _ := signing.Generate()
	err := store.ConsumeEnrollmentCode(ctx, "stale", time.Now(), model.Device{
		ID: "dev_x", TenantID: "ten_a", PublicKey: key.Public(), EnrolledAt: time.Now(),
	})
	if !errors.Is(err, controlapi.ErrCodeStale) {
		t.Fatalf("expired code returned %v, want ErrCodeStale", err)
	}
}

func TestPolicyVersionIncrementsInTheDatabase(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	if err := store.CreateTenant(ctx, tenant("ten_a"), defaultPolicy()); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	for range 5 {
		if err := store.ReplaceTenantPolicy(ctx, "ten_a", defaultPolicy()); err != nil {
			t.Fatalf("replace policy: %v", err)
		}
	}
	policy, err := store.GetTenantPolicy(ctx, "ten_a")
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	// One from creation plus five replacements.
	if policy.Version != 6 {
		t.Fatalf("policy version %d, want 6", policy.Version)
	}
}

func TestCancelledContextReachesTheDriver(t *testing.T) {
	store, _ := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Ping(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ping with a cancelled context returned %v, want context.Canceled", err)
	}
}
