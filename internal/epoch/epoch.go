// Package epoch defines the shared, coarse validity clock used by the
// capability issuer/verifier, route orchestrator and control plane so that
// capabilities and route plans agree on a common short-lived window without
// exchanging fine-grained timestamps (ADR-004: epoch-limited, coarse scoping).
package epoch

import "time"

// Length is the epoch duration. Short enough to bound a leaked capability,
// coarse enough to keep a large anonymity set per (audience, class, epoch).
const Length = time.Hour

// At returns the epoch number containing t.
func At(t time.Time) uint64 {
	return uint64(t.UTC().Unix()) / uint64(Length.Seconds())
}

// Current returns the epoch containing now.
func Current() uint64 { return At(time.Now()) }

// Window returns the currently-accepted epochs: current and previous, to
// tolerate clock skew and issuance near a boundary.
func Window(now time.Time) []uint64 {
	c := At(now)
	if c == 0 {
		return []uint64{c}
	}
	return []uint64{c, c - 1}
}
