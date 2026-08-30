package controlapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantmindSSI/haechi/internal/model"
	"github.com/QuantmindSSI/haechi/internal/signing"
)

// ctx is the background context for tests; production paths carry a request context.
var ctx = context.Background()

func newTestService(t *testing.T) *Service {
	t.Helper()
	signer, err := signing.Generate()
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	// nil engine -> StaticEngine (deny-by-default) dev adapter.
	return NewService(NewMemoryStore(), nil, signer, "control-api-key-1")
}

// enroll performs a correct proof-of-possession enrolment.
func enroll(t *testing.T, svc *Service, tenantID string) (model.Device, signing.PrivateKey) {
	t.Helper()
	code, err := svc.IssueEnrollmentCode(ctx, tenantID)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}
	devKey, _ := signing.Generate()
	msg := EnrollmentProofMessage(devKey.Public(), code.Challenge)
	proof, _ := devKey.Sign(signing.ContextEnrollmentChallenge, msg)
	dev, err := svc.EnrollDevice(ctx, EnrollRequest{
		Code: code.Code, PublicKey: devKey.Public(), Platform: "linux", Proof: proof,
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	return dev, devKey
}

func TestEnrollAndDenyByDefault(t *testing.T) {
	svc := newTestService(t)
	tenant, _ := svc.CreateTenant(ctx, "Acme", "https://idp.acme")
	dev, _ := enroll(t, svc, tenant.ID)

	// Fresh tenant has no allowed service classes => deny by default.
	if d := svc.Authorize(ctx, dev.ID, "general"); d.Allow {
		t.Fatalf("expected deny-by-default, got allow")
	}

	if err := svc.SetTenantPolicy(ctx, tenant.ID, TenantPolicy{
		AllowedServiceClasses: []string{"general"},
		AllowedTransports:     []model.Transport{model.TransportDirect},
		PQRequirement:         model.PQPreferred,
		KillSwitch:            true,
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	if d := svc.Authorize(ctx, dev.ID, "general"); !d.Allow {
		t.Fatalf("expected allow after policy, got deny: %s", d.Reason)
	}
	if d := svc.Authorize(ctx, dev.ID, "premium"); d.Allow {
		t.Fatalf("premium not permitted, but allowed")
	}
}

func TestCreateTenantRejectsInvalidInput(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.CreateTenant(ctx, "   ", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank name error = %v, want ErrInvalid", err)
	}
	if _, err := svc.CreateTenant(ctx, "Acme", "not-a-url"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid issuer error = %v, want ErrInvalid", err)
	}
}

func TestRevokedDeviceDenied(t *testing.T) {
	svc := newTestService(t)
	tenant, _ := svc.CreateTenant(ctx, "Acme", "")
	dev, _ := enroll(t, svc, tenant.ID)
	if err := svc.SetTenantPolicy(ctx, tenant.ID, TenantPolicy{
		AllowedServiceClasses: []string{"general"},
		PQRequirement:         model.PQPreferred,
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	if d := svc.Authorize(ctx, dev.ID, "general"); !d.Allow {
		t.Fatal("should be allowed before revoke")
	}
	if err := svc.RevokeDevice(ctx, dev.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if d := svc.Authorize(ctx, dev.ID, "general"); d.Allow {
		t.Fatal("revoked device must be denied")
	}
}

func TestEnrollmentBadProofRejected(t *testing.T) {
	svc := newTestService(t)
	tenant, _ := svc.CreateTenant(ctx, "Acme", "")
	code, _ := svc.IssueEnrollmentCode(ctx, tenant.ID)

	devKey, _ := signing.Generate()
	// Sign the WRONG message (empty challenge) -> proof must fail.
	badProof, _ := devKey.Sign(signing.ContextEnrollmentChallenge, []byte("wrong"))
	if _, err := svc.EnrollDevice(ctx, EnrollRequest{
		Code: code.Code, PublicKey: devKey.Public(), Platform: "linux", Proof: badProof,
	}); err != ErrBadProof {
		t.Fatalf("want ErrBadProof, got %v", err)
	}
}

func TestEnrollmentCodeSingleUse(t *testing.T) {
	svc := newTestService(t)
	tenant, _ := svc.CreateTenant(ctx, "Acme", "")
	code, _ := svc.IssueEnrollmentCode(ctx, tenant.ID)

	devKey, _ := signing.Generate()
	msg := EnrollmentProofMessage(devKey.Public(), code.Challenge)
	proof, _ := devKey.Sign(signing.ContextEnrollmentChallenge, msg)
	if _, err := svc.EnrollDevice(ctx, EnrollRequest{Code: code.Code, PublicKey: devKey.Public(), Proof: proof}); err != nil {
		t.Fatalf("first enrol: %v", err)
	}
	// Reusing the same code must fail.
	dev2, _ := signing.Generate()
	msg2 := EnrollmentProofMessage(dev2.Public(), code.Challenge)
	proof2, _ := dev2.Sign(signing.ContextEnrollmentChallenge, msg2)
	if _, err := svc.EnrollDevice(ctx, EnrollRequest{Code: code.Code, PublicKey: dev2.Public(), Proof: proof2}); err != ErrCodeUsed {
		t.Fatalf("want ErrCodeUsed, got %v", err)
	}
}

func TestEnrollmentCodeStoredAsOneWayLookupKey(t *testing.T) {
	store := NewMemoryStore()
	signer, err := signing.Generate()
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	svc := NewService(store, nil, signer, "test-key")
	tenant, err := svc.CreateTenant(ctx, "Acme", "")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	code, err := svc.IssueEnrollmentCode(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}
	if _, err := store.GetEnrollmentCode(ctx, code.Code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("raw enrollment code was stored: %v", err)
	}
	if _, err := store.GetEnrollmentCode(ctx, enrollmentCodeKey(code.Code)); err != nil {
		t.Fatalf("hashed enrollment code lookup failed: %v", err)
	}
}

func TestEnrollmentCodeSingleUseConcurrent(t *testing.T) {
	svc := newTestService(t)
	tenant, err := svc.CreateTenant(ctx, "Acme", "")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	code, err := svc.IssueEnrollmentCode(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}
	devKey, err := signing.Generate()
	if err != nil {
		t.Fatalf("device key: %v", err)
	}
	proof, err := devKey.Sign(
		signing.ContextEnrollmentChallenge,
		EnrollmentProofMessage(devKey.Public(), code.Challenge),
	)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}

	const attempts = 16
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, enrollErr := svc.EnrollDevice(ctx, EnrollRequest{
				Code: code.Code, PublicKey: devKey.Public(), Platform: "linux", Proof: proof,
			})
			results <- enrollErr
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	used := 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrCodeUsed):
			used++
		default:
			t.Fatalf("unexpected enrollment result: %v", result)
		}
	}
	if successes != 1 || used != attempts-1 {
		t.Fatalf("successes=%d used=%d, want 1 and %d", successes, used, attempts-1)
	}
}

func TestMemoryStoreScopesGatewaysByTenant(t *testing.T) {
	store := NewMemoryStore()
	if err := store.UpsertGateway(ctx, model.Gateway{ID: "gw-a", TenantID: "tenant-a"}); err != nil {
		t.Fatalf("store tenant-a gateway: %v", err)
	}
	if err := store.UpsertGateway(ctx, model.Gateway{ID: "gw-b", TenantID: "tenant-b"}); err != nil {
		t.Fatalf("store tenant-b gateway: %v", err)
	}
	if err := store.UpsertGateway(ctx, model.Gateway{ID: "gw-a", TenantID: "tenant-b"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-tenant gateway update error = %v, want ErrConflict", err)
	}

	gateways, err := store.ListGateways(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list gateways: %v", err)
	}
	if len(gateways) != 1 || gateways[0].ID != "gw-a" {
		t.Fatalf("tenant-a gateways = %+v, want only gw-a", gateways)
	}
}

func TestMemoryStoreRejectsEnrollmentCodeCollision(t *testing.T) {
	store := NewMemoryStore()
	code := EnrollmentCode{Code: "COLLISION", TenantID: "tenant-a"}
	if err := store.PutEnrollmentCode(ctx, code); err != nil {
		t.Fatalf("first code insert: %v", err)
	}
	if err := store.PutEnrollmentCode(ctx, code); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate code error = %v, want ErrConflict", err)
	}
}

func TestPolicyVersionsAdvanceAtomically(t *testing.T) {
	svc := newTestService(t)
	tenant, err := svc.CreateTenant(ctx, "Acme", "")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	const updates = 16
	results := make(chan error, updates)
	var wg sync.WaitGroup
	for range updates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- svc.SetTenantPolicy(ctx, tenant.ID, TenantPolicy{
				AllowedServiceClasses: []string{"general"},
				PQRequirement:         model.PQPreferred,
				KillSwitch:            true,
			})
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result != nil {
			t.Fatalf("policy update: %v", result)
		}
	}
	bundle, err := svc.PolicyBundle(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("policy bundle: %v", err)
	}
	if bundle.Bundle.Version != updates+1 {
		t.Fatalf("policy version = %d, want %d", bundle.Bundle.Version, updates+1)
	}
}

func TestEnrollmentCodeExpiry(t *testing.T) {
	svc := newTestService(t)
	now := time.Now()
	svc.SetClock(func() time.Time { return now })
	tenant, _ := svc.CreateTenant(ctx, "Acme", "")
	code, _ := svc.IssueEnrollmentCode(ctx, tenant.ID)

	// Advance beyond TTL.
	svc.SetClock(func() time.Time { return now.Add(30 * time.Minute) })
	devKey, _ := signing.Generate()
	msg := EnrollmentProofMessage(devKey.Public(), code.Challenge)
	proof, _ := devKey.Sign(signing.ContextEnrollmentChallenge, msg)
	if _, err := svc.EnrollDevice(ctx, EnrollRequest{Code: code.Code, PublicKey: devKey.Public(), Proof: proof}); err != ErrCodeStale {
		t.Fatalf("want ErrCodeStale, got %v", err)
	}
}

func TestPolicyBundleSignedAndVersioned(t *testing.T) {
	svc := newTestService(t)
	tenant, _ := svc.CreateTenant(ctx, "Acme", "")
	if err := svc.SetTenantPolicy(ctx, tenant.ID, TenantPolicy{
		AllowedServiceClasses: []string{"general"},
		PQRequirement:         model.PQPreferred,
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	bundle, err := svc.PolicyBundle(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if !bundle.Verify(svc.SignerPub()) {
		t.Fatal("policy bundle signature invalid")
	}
	// CreateTenant wrote version 1; SetTenantPolicy bumped to 2.
	if bundle.Bundle.Version != 2 {
		t.Fatalf("want version 2, got %d", bundle.Bundle.Version)
	}
	// Tampering must break the signature.
	bundle.Bundle.KillSwitch = !bundle.Bundle.KillSwitch
	if bundle.Verify(svc.SignerPub()) {
		t.Fatal("tampered bundle verified")
	}
	bundle.Bundle.KillSwitch = !bundle.Bundle.KillSwitch
	bundle.SignerKeyID = "substituted-key"
	if bundle.Verify(svc.SignerPub()) {
		t.Fatal("bundle with substituted signer key id verified")
	}
}

func TestInvalidPolicyAndGatewayRejected(t *testing.T) {
	svc := newTestService(t)
	tenant, err := svc.CreateTenant(ctx, "Acme", "")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := svc.SetTenantPolicy(ctx, tenant.ID, TenantPolicy{PQRequirement: "unsupported"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid PQ mode error = %v, want ErrInvalid", err)
	}
	if err := svc.RegisterGateway(ctx, model.Gateway{
		ID: "gw-1", TenantID: tenant.ID, Audience: "eu-standard", Endpoint: "not-an-endpoint",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid gateway error = %v, want ErrInvalid", err)
	}
}
