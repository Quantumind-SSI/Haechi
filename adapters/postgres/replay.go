// Replay store for gateway-side capability admission.
//
// Schema SSOT: deploy/postgres/replay-schema.sql. That schema is deliberately
// separate from the control-plane schema in this same directory: a gateway must
// not hold credentials to the database carrying tenant and device identity, or
// the trust separation in ADR-004 stops meaning anything. Point this adapter at
// its own database, or at least its own least-privilege role.
//
// Like the control-plane store, this depends only on database/sql. The driver
// is selected by the composition root.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/QuantmindSSI/haechi/pkg/capability"
)

// ReplayStore implements capability.ReplayCache over PostgreSQL, making a
// capability single-use across every verifier process that shares the database.
//
// It satisfies the three properties the release blocker requires. Atomicity
// comes from the (epoch, nonce) primary key rather than from a read followed by
// a write, so concurrent verifiers cannot both admit. Durability comes from the
// table surviving process restarts. Expiry comes from PurgeBefore.
type ReplayStore struct{ db *sql.DB }

var (
	_ capability.ReplayCache  = (*ReplayStore)(nil)
	_ capability.ReplayPurger = (*ReplayStore)(nil)
)

// NewReplayStore wraps an established *sql.DB.
//
// The database must have deploy/postgres/replay-schema.sql applied, and must
// not be the control-plane database.
func NewReplayStore(db *sql.DB) *ReplayStore { return &ReplayStore{db: db} }

// errEpochRange reports an epoch that cannot be represented in the BIGINT
// column. Kept unexported: callers only ever need to know that the spend
// failed, and every failure denies.
var errEpochRange = errors.New("postgres: epoch exceeds the representable range")

// checkedEpoch converts a Go uint64 epoch to the int64 the column stores.
//
// An epoch above math.MaxInt64 would wrap to a negative value and land in a
// different region of the key space, where it could not collide with the
// records that prove single use. Wrapping is therefore a correctness failure,
// not a rounding detail, and is rejected. At one epoch per hour the real clock
// reaches this bound in roughly a thousand billion years; a value this large
// means a corrupted or hostile input.
func checkedEpoch(epoch uint64) (int64, error) {
	if epoch > math.MaxInt64 {
		return 0, fmt.Errorf("%w: %d", errEpochRange, epoch)
	}
	return int64(epoch), nil
}

// Spend records (epoch, nonce) exactly once.
//
// Returns capability.ErrReplay when the token was already spent, and any other
// error when the record could not be durably written. The verifier denies
// admission for every error, so a database outage fails closed rather than
// admitting unverified traffic.
//
// Complexity: one indexed insert. Blocking I/O, bounded by ctx.
func (r *ReplayStore) Spend(ctx context.Context, epoch uint64, nonce []byte) error {
	if len(nonce) == 0 || len(nonce) > capability.MaxNonceLen {
		return capability.ErrNonceLength
	}
	stored, err := checkedEpoch(epoch)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO capability_replay(epoch,nonce) VALUES($1,$2)
		 ON CONFLICT (epoch,nonce) DO NOTHING`, stored, nonce)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// The conflict target matched, so this exact token was already spent.
		return capability.ErrReplay
	}
	return nil
}

// PurgeBefore deletes records for epochs strictly below the given epoch.
//
// Call it with the oldest epoch the verifier still accepts, which
// epoch.Window supplies. A record below that can never authorise anything
// again, so removing it is safe; keeping it only grows the table.
//
// Complexity: one indexed range delete. Blocking I/O, bounded by ctx.
func (r *ReplayStore) PurgeBefore(ctx context.Context, epoch uint64) (int64, error) {
	stored, err := checkedEpoch(epoch)
	if err != nil {
		return 0, err
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM capability_replay WHERE epoch < $1`, stored)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Ping verifies the replay database is reachable, for gateway readiness.
//
// A gateway that cannot reach its replay store cannot admit anything, because
// Spend fails closed. Readiness must report that rather than let the gateway
// advertise capacity it does not have.
func (r *ReplayStore) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }
