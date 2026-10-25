package capability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestMemoryReplaySpendsOnce(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryReplayCache()

	if err := cache.Spend(ctx, 7, []byte("nonce")); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := cache.Spend(ctx, 7, []byte("nonce")); !errors.Is(err, ErrReplay) {
		t.Fatalf("second spend: got %v, want ErrReplay", err)
	}
}

// TestMemoryReplaySeparatesEpochs guards the partitioning the store depends on:
// the same nonce in a different epoch is a different record, so an epoch
// rollover must not deny a fresh token.
func TestMemoryReplaySeparatesEpochs(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryReplayCache()

	if err := cache.Spend(ctx, 7, []byte("nonce")); err != nil {
		t.Fatalf("epoch 7: %v", err)
	}
	if err := cache.Spend(ctx, 8, []byte("nonce")); err != nil {
		t.Fatalf("epoch 8 must be independent: %v", err)
	}
}

func TestMemoryReplayRejectsUnusableNonces(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryReplayCache()

	if err := cache.Spend(ctx, 1, nil); !errors.Is(err, ErrNonceLength) {
		t.Fatalf("empty nonce: got %v, want ErrNonceLength", err)
	}
	oversized := bytes.Repeat([]byte{0xAB}, MaxNonceLen+1)
	if err := cache.Spend(ctx, 1, oversized); !errors.Is(err, ErrNonceLength) {
		t.Fatalf("oversized nonce: got %v, want ErrNonceLength", err)
	}
	if err := cache.Spend(ctx, 1, bytes.Repeat([]byte{0xAB}, MaxNonceLen)); err != nil {
		t.Fatalf("a nonce exactly at the bound must be accepted: %v", err)
	}
}

// TestMemoryReplayTreatsCancellationAsFailure is the fail-closed property. A
// cancelled context must never look like a successful spend, or a verifier
// under load would admit tokens it never recorded.
func TestMemoryReplayTreatsCancellationAsFailure(t *testing.T) {
	cache := NewMemoryReplayCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := cache.Spend(ctx, 1, []byte("nonce")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	// The token must remain unspent, so a later attempt with a live context
	// succeeds rather than reporting a replay that never happened.
	if err := cache.Spend(context.Background(), 1, []byte("nonce")); err != nil {
		t.Fatalf("a cancelled spend must not record anything: %v", err)
	}
}

func TestMemoryReplayFailsClosedAtCapacity(t *testing.T) {
	ctx := context.Background()
	cache := &MemoryReplayCache{seen: map[uint64]map[string]struct{}{}, max: 1}

	if err := cache.Spend(ctx, 42, []byte("first")); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := cache.Spend(ctx, 42, []byte("second")); !errors.Is(err, ErrReplayCacheFull) {
		t.Fatalf("got %v, want ErrReplayCacheFull", err)
	}
	// A full cache must still detect a replay of what it already holds.
	if err := cache.Spend(ctx, 42, []byte("first")); !errors.Is(err, ErrReplay) {
		t.Fatalf("got %v, want ErrReplay", err)
	}
}

func TestMemoryReplayPurgeReleasesExpiredEpochs(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryReplayCache()

	for _, epoch := range []uint64{5, 6, 7} {
		if err := cache.Spend(ctx, epoch, []byte("nonce")); err != nil {
			t.Fatalf("spend epoch %d: %v", epoch, err)
		}
	}

	removed, err := cache.PurgeBefore(ctx, 7)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed %d records, want 2", removed)
	}
	// Epoch 7 is still in the window, so it must still deny a replay.
	if err := cache.Spend(ctx, 7, []byte("nonce")); !errors.Is(err, ErrReplay) {
		t.Fatalf("epoch 7 should still be held: got %v", err)
	}
	// Purged epochs are forgotten. That is safe: the verifier no longer
	// accepts them, so the token cannot be presented again anyway.
	if err := cache.Spend(ctx, 5, []byte("nonce")); err != nil {
		t.Fatalf("purged epoch should be reusable storage: %v", err)
	}
}

// TestMemoryReplayPurgeReclaimsCapacity proves the bound and the purge agree.
// If PurgeBefore did not decrement the counter, a long-running verifier would
// eventually refuse every token despite holding almost nothing.
func TestMemoryReplayPurgeReclaimsCapacity(t *testing.T) {
	ctx := context.Background()
	cache := &MemoryReplayCache{seen: map[uint64]map[string]struct{}{}, max: 2}

	if err := cache.Spend(ctx, 1, []byte("a")); err != nil {
		t.Fatalf("spend a: %v", err)
	}
	if err := cache.Spend(ctx, 1, []byte("b")); err != nil {
		t.Fatalf("spend b: %v", err)
	}
	if err := cache.Spend(ctx, 2, []byte("c")); !errors.Is(err, ErrReplayCacheFull) {
		t.Fatalf("expected the cache to be full, got %v", err)
	}
	if _, err := cache.PurgeBefore(ctx, 2); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if err := cache.Spend(ctx, 2, []byte("c")); err != nil {
		t.Fatalf("capacity was not reclaimed by the purge: %v", err)
	}
}

// TestMemoryReplayAdmitsOnceUnderConcurrency is the property the whole store
// exists for. Many goroutines present the same token at once and exactly one
// may be told it recorded it.
func TestMemoryReplayAdmitsOnceUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryReplayCache()

	const racers = 64
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := cache.Spend(ctx, 9, []byte("contended"))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				admitted++
			case errors.Is(err, ErrReplay):
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if admitted != 1 {
		t.Fatalf("%d goroutines recorded the spend, want exactly 1", admitted)
	}
}

// TestVerifierDeniesWhenTheReplayStoreFails proves the fail-closed contract at
// the verifier boundary: a store outage must deny admission, not bypass the
// single-use check.
func TestVerifierDeniesWhenTheReplayStoreFails(t *testing.T) {
	is := NewIssuer(2048)
	attrs := Attributes{Audience: "gw-lon", ServiceClass: "standard", Epoch: 100}
	kid, pub, err := is.PublicKey(attrs)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	bl, err := Prepare(kid, pub, attrs)
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

	outage := errors.New("replay store unreachable")
	v := NewVerifier("gw-lon", []string{"standard"}, []uint64{100},
		[]VerifierKey{{Pub: pub, Attrs: attrs}}, failingReplay{err: outage})

	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, outage) {
		t.Fatalf("got %v, want the store error to deny admission", err)
	}
}

// failingReplay is a store that cannot record anything.
type failingReplay struct{ err error }

func (f failingReplay) Spend(context.Context, uint64, []byte) error { return f.err }

// TestMemoryReplayHoldsDistinctNonces is a sanity check that the cache is
// keyed on the nonce and not only on the epoch.
func TestMemoryReplayHoldsDistinctNonces(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryReplayCache()
	for i := 0; i < 32; i++ {
		nonce := []byte(fmt.Sprintf("nonce-%d", i))
		if err := cache.Spend(ctx, 3, nonce); err != nil {
			t.Fatalf("spend %d: %v", i, err)
		}
	}
	if err := cache.Spend(ctx, 3, []byte("nonce-0")); !errors.Is(err, ErrReplay) {
		t.Fatalf("got %v, want ErrReplay for a repeated nonce", err)
	}
}
