// Package shaping provides Release-1 deterministic traffic-shaping profiles
// (Integration Document §6.5). Shaping adjusts observable metadata such as
// padding, pacing, and batching within bounded limits. It MUST NEVER rewrite authenticated
// ciphertext or generate unbounded cover traffic. Any cover connection
// terminates at a Haechi-controlled endpoint (not modelled here).
package shaping

// Profile is a bounded, deterministic shaping configuration.
type Profile struct {
	Name         string
	PaddingBytes int // fixed padding added to observable frame size, bounded
	PaceMillis   int // minimum inter-send pacing, bounded
	BatchSize    int // frames coalesced before send, bounded
}

// Hard bounds ensure shaping cannot harm reliability (Risk table §15).
const (
	maxPadding = 512
	maxPace    = 50
	maxBatch   = 16
)

// Off disables shaping (direct-mode default).
func Off() Profile { return Profile{Name: "off"} }

// Standard is the deterministic baseline profile.
func Standard() Profile {
	return Profile{Name: "standard", PaddingBytes: 256, PaceMillis: 5, BatchSize: 4}
}

// Clamp enforces the hard upper bounds regardless of input, so a bad policy can
// never produce an unbounded or reliability-harming profile.
func (p Profile) Clamp() Profile {
	if p.PaddingBytes < 0 {
		p.PaddingBytes = 0
	}
	if p.PaddingBytes > maxPadding {
		p.PaddingBytes = maxPadding
	}
	if p.PaceMillis < 0 {
		p.PaceMillis = 0
	}
	if p.PaceMillis > maxPace {
		p.PaceMillis = maxPace
	}
	if p.BatchSize < 0 {
		p.BatchSize = 0
	}
	if p.BatchSize > maxBatch {
		p.BatchSize = maxBatch
	}
	return p
}
