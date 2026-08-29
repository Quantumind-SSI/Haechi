// Package gatewayctl is the data-plane gateway admission controller
// (Integration Document §4, §9 step 8). It verifies a presented capability
// locally (no identity-provider call), applies coarse abuse limits, and never
// records durable user identity. On success it would program the WireGuard peer
// and controlled egress; here it returns the authenticated coarse attributes.
package gatewayctl

import (
	"errors"
	"sync"

	"github.com/QuantmindSSI/haechi/pkg/capability"
)

var (
	// ErrRateLimited denies admission when a coarse per-epoch budget is exceeded.
	ErrRateLimited = errors.New("gatewayctl: admission rate limit exceeded")
	// ErrVerifierUnavailable denies admission when no capability verifier exists.
	ErrVerifierUnavailable = errors.New("gatewayctl: capability verifier unavailable")
)

// Limiter is a coarse abuse control. It sees only anonymous, aggregate counts;
// never user identity.
type Limiter interface {
	Allow(epoch uint64) bool
}

// Controller admits capabilities for one gateway.
type Controller struct {
	GatewayID string
	Audience  string
	verifier  *capability.Verifier
	limiter   Limiter
}

// New builds a gateway controller around a capability verifier.
func New(gatewayID, audience string, verifier *capability.Verifier, limiter Limiter) *Controller {
	if limiter == nil {
		limiter = DenyAll{}
	}
	return &Controller{GatewayID: gatewayID, Audience: audience, verifier: verifier, limiter: limiter}
}

// Admit verifies a capability token and applies abuse control. On success it
// returns the coarse attributes the gateway may act on. Fail closed otherwise.
func (c *Controller) Admit(tok *capability.Token) (capability.Attributes, error) {
	if c.verifier == nil {
		return capability.Attributes{}, ErrVerifierUnavailable
	}
	attrs, err := c.verifier.Verify(tok)
	if err != nil {
		return capability.Attributes{}, err
	}
	if !c.limiter.Allow(attrs.Epoch) {
		return capability.Attributes{}, ErrRateLimited
	}
	return attrs, nil
}

// NoLimit permits every verified capability. It is intended only for explicit
// test configurations.
type NoLimit struct{}

// Allow implements Limiter.
func (NoLimit) Allow(uint64) bool { return true }

// DenyAll is the fail-closed default when no abuse limiter is configured.
type DenyAll struct{}

// Allow implements Limiter.
func (DenyAll) Allow(uint64) bool { return false }

// EpochBudget caps admissions per epoch to bound abuse of anonymous tokens.
type EpochBudget struct {
	mu           sync.Mutex
	max          int
	highestEpoch uint64
	initialized  bool
	counts       map[uint64]int
}

// NewEpochBudget caps admissions to max per epoch.
func NewEpochBudget(max int) *EpochBudget {
	return &EpochBudget{max: max, counts: make(map[uint64]int, 2)}
}

// Allow implements Limiter with a per-epoch reset.
func (b *EpochBudget) Allow(ep uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 {
		return false
	}
	if !b.initialized || ep > b.highestEpoch {
		b.highestEpoch = ep
		b.initialized = true
		for knownEpoch := range b.counts {
			if ep > 0 && knownEpoch < ep-1 {
				delete(b.counts, knownEpoch)
			}
		}
	}
	if b.highestEpoch > 0 && ep < b.highestEpoch-1 {
		return false
	}
	if b.counts[ep] >= b.max {
		return false
	}
	b.counts[ep]++
	return true
}
