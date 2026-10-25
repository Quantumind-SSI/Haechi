// Integration tests for the PostgreSQL replay store.
//
// These run only when HAECHI_REPLAY_TEST_DATABASE_URL points at a disposable
// PostgreSQL database. The variable is distinct from the control-plane one
// because the two schemas are deliberately deployed to different databases: a
// gateway must not hold credentials to identity-linked control-plane state
// (deploy/postgres/replay-schema.sql, ADR-004). Pointing both at one database
// would let the tests pass while the deployment they describe is unsafe.
//
//	createdb -h 127.0.0.1 -p 55432 -U postgres haechi_replay
//	export HAECHI_REPLAY_TEST_DATABASE_URL="postgresql://postgres@127.0.0.1:55432/haechi_replay"
//
// The behaviour proven here cannot be proven against the memory store: single
// use across independent connections, survival of a process restart, and
// context cancellation reaching the driver.
package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/QuantmindSSI/haechi/adapters/postgres"
	"github.com/QuantmindSSI/haechi/pkg/capability"
)

// openReplayDB returns a fresh connection to the replay database with the
// schema applied and the table emptied.
func openReplayDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("HAECHI_REPLAY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HAECHI_REPLAY_TEST_DATABASE_URL is not set")
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

	schema, err := os.ReadFile(filepath.Join("..", "..", "deploy", "postgres", "replay-schema.sql"))
	if err != nil {
		t.Fatalf("read replay schema: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), string(schema)); err != nil {
		t.Fatalf("apply replay schema: %v", err)
	}
	return db
}

func newReplayStore(t *testing.T) *postgres.ReplayStore {
	t.Helper()
	db := openReplayDB(t)
	if _, err := db.ExecContext(context.Background(), "DELETE FROM capability_replay"); err != nil {
		t.Fatalf("clear capability_replay: %v", err)
	}
	return postgres.NewReplayStore(db)
}

func TestReplayStorePingReportsReachability(t *testing.T) {
	store := newReplayStore(t)
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestReplayStoreSpendsOnce(t *testing.T) {
	store := newReplayStore(t)
	ctx := context.Background()

	if err := store.Spend(ctx, 7, []byte("nonce")); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := store.Spend(ctx, 7, []byte("nonce")); !errors.Is(err, capability.ErrReplay) {
		t.Fatalf("second spend: got %v, want ErrReplay", err)
	}
}

func TestReplayStoreSeparatesEpochsAndNonces(t *testing.T) {
	store := newReplayStore(t)
	ctx := context.Background()

	if err := store.Spend(ctx, 7, []byte("nonce")); err != nil {
		t.Fatalf("epoch 7: %v", err)
	}
	if err := store.Spend(ctx, 8, []byte("nonce")); err != nil {
		t.Fatalf("same nonce in a later epoch must be independent: %v", err)
	}
	if err := store.Spend(ctx, 7, []byte("other")); err != nil {
		t.Fatalf("different nonce in the same epoch must be independent: %v", err)
	}
}

// TestReplayStoreAdmitsOnceAcrossConnections is the property that justifies
// this adapter. Each racer uses its own store over its own pool, standing in
// for separate gateway processes, and exactly one may record the spend.
func TestReplayStoreAdmitsOnceAcrossConnections(t *testing.T) {
	if os.Getenv("HAECHI_REPLAY_TEST_DATABASE_URL") == "" {
		t.Skip("HAECHI_REPLAY_TEST_DATABASE_URL is not set")
	}
	// Clear once through a dedicated connection before the racers start.
	newReplayStore(t)

	const racers = 16
	stores := make([]*postgres.ReplayStore, racers)
	for i := range stores {
		stores[i] = postgres.NewReplayStore(openReplayDB(t))
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(s *postgres.ReplayStore) {
			defer wg.Done()
			<-start
			err := s.Spend(context.Background(), 11, []byte("contended"))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				admitted++
			case errors.Is(err, capability.ErrReplay):
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(stores[i])
	}
	close(start)
	wg.Wait()

	if admitted != 1 {
		t.Fatalf("%d independent connections recorded the spend, want exactly 1", admitted)
	}
}

// TestReplayStoreSurvivesRestart closes the connection the spend was made on
// and reopens, standing in for a gateway process restart. A process-local
// cache loses this and would admit the token a second time.
func TestReplayStoreSurvivesRestart(t *testing.T) {
	first := newReplayStore(t)
	ctx := context.Background()

	if err := first.Spend(ctx, 12, []byte("durable")); err != nil {
		t.Fatalf("spend: %v", err)
	}

	second := postgres.NewReplayStore(openReplayDB(t))
	if err := second.Spend(ctx, 12, []byte("durable")); !errors.Is(err, capability.ErrReplay) {
		t.Fatalf("after restart: got %v, want ErrReplay", err)
	}
}

func TestReplayStorePurgeReleasesExpiredEpochs(t *testing.T) {
	store := newReplayStore(t)
	ctx := context.Background()

	for _, epoch := range []uint64{5, 6, 7} {
		if err := store.Spend(ctx, epoch, []byte("nonce")); err != nil {
			t.Fatalf("spend epoch %d: %v", epoch, err)
		}
	}

	removed, err := store.PurgeBefore(ctx, 7)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed %d records, want 2", removed)
	}
	if err := store.Spend(ctx, 7, []byte("nonce")); !errors.Is(err, capability.ErrReplay) {
		t.Fatalf("epoch 7 is still in the window: got %v", err)
	}
	if err := store.Spend(ctx, 5, []byte("nonce")); err != nil {
		t.Fatalf("purged epoch should be reusable storage: %v", err)
	}
}

func TestReplayStoreRejectsUnusableNonces(t *testing.T) {
	store := newReplayStore(t)
	ctx := context.Background()

	if err := store.Spend(ctx, 1, nil); !errors.Is(err, capability.ErrNonceLength) {
		t.Fatalf("empty nonce: got %v, want ErrNonceLength", err)
	}
	oversized := make([]byte, capability.MaxNonceLen+1)
	if err := store.Spend(ctx, 1, oversized); !errors.Is(err, capability.ErrNonceLength) {
		t.Fatalf("oversized nonce: got %v, want ErrNonceLength", err)
	}
}

// TestReplayStoreRejectsUnrepresentableEpoch guards the uint64 to BIGINT
// narrowing. Wrapping would move the record into a region of the key space
// where it could not collide with the records proving single use.
func TestReplayStoreRejectsUnrepresentableEpoch(t *testing.T) {
	store := newReplayStore(t)
	ctx := context.Background()

	const tooLarge = uint64(1) << 63
	if err := store.Spend(ctx, tooLarge, []byte("nonce")); err == nil {
		t.Fatal("an epoch above math.MaxInt64 must be rejected, not wrapped")
	}
	if _, err := store.PurgeBefore(ctx, tooLarge); err == nil {
		t.Fatal("PurgeBefore must reject an unrepresentable epoch too")
	}
}

// TestReplayStoreCancellationReachesTheDriver proves the context is honoured
// all the way down, and that a cancelled spend records nothing.
func TestReplayStoreCancellationReachesTheDriver(t *testing.T) {
	store := newReplayStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Spend(ctx, 13, []byte("cancelled")); err == nil {
		t.Fatal("a cancelled context must fail the spend")
	}

	// Fail closed means the token was never recorded, so a live context can
	// still spend it. The opposite would deny a legitimate first presentation.
	if err := store.Spend(context.Background(), 13, []byte("cancelled")); err != nil {
		t.Fatalf("a cancelled spend must not record anything: %v", err)
	}
}

func TestReplayStoreDeadlineIsHonoured(t *testing.T) {
	store := newReplayStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	if err := store.Spend(ctx, 14, []byte("expired")); err == nil {
		t.Fatal("an expired deadline must fail the spend")
	}
}

// TestReplayStoreSatisfiesTheVerifier wires the real store into a real
// verifier and checks the end-to-end single-use property, including that the
// second presentation of a valid token is denied.
func TestReplayStoreSatisfiesTheVerifier(t *testing.T) {
	store := newReplayStore(t)
	ctx := context.Background()

	is := capability.NewIssuer(2048)
	attrs := capability.Attributes{Audience: "gw-lon", ServiceClass: "standard", Epoch: 21}
	kid, pub, err := is.PublicKey(attrs)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	bl, err := capability.Prepare(kid, pub, attrs)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	_, blindSig, err := is.BlindSign(attrs, bl.Blinded)
	if err != nil {
		t.Fatalf("blind sign: %v", err)
	}
	tok, err := bl.Finalize(blindSig)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}

	verifier := capability.NewVerifier("gw-lon", []string{"standard"}, []uint64{21},
		[]capability.VerifierKey{{Pub: pub, Attrs: attrs}}, store)

	got, err := verifier.Verify(ctx, tok)
	if err != nil {
		t.Fatalf("first admission: %v", err)
	}
	if got.Audience != "gw-lon" || got.ServiceClass != "standard" {
		t.Fatalf("unexpected attributes: %+v", got)
	}
	if _, err := verifier.Verify(ctx, tok); !errors.Is(err, capability.ErrReplay) {
		t.Fatalf("second admission: got %v, want ErrReplay", err)
	}
}

// TestReplayStoreHoldsManyDistinctTokens exercises the index under a realistic
// spread of nonces rather than a single contended key.
func TestReplayStoreHoldsManyDistinctTokens(t *testing.T) {
	store := newReplayStore(t)
	ctx := context.Background()

	const tokens = 200
	for i := 0; i < tokens; i++ {
		if err := store.Spend(ctx, 31, []byte(fmt.Sprintf("nonce-%03d", i))); err != nil {
			t.Fatalf("spend %d: %v", i, err)
		}
	}
	if err := store.Spend(ctx, 31, []byte("nonce-000")); !errors.Is(err, capability.ErrReplay) {
		t.Fatalf("got %v, want ErrReplay", err)
	}
	removed, err := store.PurgeBefore(ctx, 32)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != tokens {
		t.Fatalf("purged %d records, want %d", removed, tokens)
	}
}
