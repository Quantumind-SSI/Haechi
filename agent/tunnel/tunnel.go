// Package tunnel defines the Haechi tunnel abstraction and adapters. The agent
// core coordinates tunnels only through this interface; platform specifics
// (wireguard-go on desktop, WireGuardKit on Apple, VpnService on Android) live
// behind adapters and never change core behaviour (ADR-003).
//
// Post-quantum reinforcement is negotiated here per ADR-005: an adapter reports
// the negotiated mode, and a "required" configuration MUST fail closed if the
// hybrid PQ exchange is unavailable.
package tunnel

import (
	"errors"

	"github.com/QuantmindSSI/haechi/internal/model"
)

// ErrPQRequiredUnavailable is returned when policy requires hybrid PQ but the
// adapter cannot negotiate it. Fail closed; never permit a silent downgrade.
var ErrPQRequiredUnavailable = errors.New("tunnel: post-quantum reinforcement required but unavailable")

// Config describes a tunnel to bring up.
type Config struct {
	GatewayID          string
	GatewayEndpoint    string
	GatewayWGPublicKey string
	DNS                []string
	PQRequirement      model.PQMode
}

// Status is the observable tunnel state. NegotiatedPQ is exposed to operators
// without revealing keys or user activity (ADR-005).
type Status struct {
	Up           bool
	GatewayID    string
	NegotiatedPQ model.PQMode // "hybrid-pq" reported as PQRequired-satisfied, or "classical"
}

// NegotiatedHybridPQ is the status value meaning hybrid PQ is active.
const NegotiatedHybridPQ = model.PQRequired // reuse "required" token to mean hybrid-pq active

// Tunnel is a platform tunnel adapter.
type Tunnel interface {
	Up(cfg Config) (Status, error)
	Down() error
	Status() Status
}

// Loopback is a test/development adapter that simulates WireGuard bring-up and
// Rosenpass PQ negotiation without touching the OS network stack. It enforces
// the ADR-005 downgrade policy so the agent's fail-closed behaviour is testable.
type Loopback struct {
	// PQAvailable simulates whether Rosenpass hybrid PQ can be negotiated.
	PQAvailable bool
	status      Status
}

// Up brings up the simulated tunnel, honouring the PQ requirement.
func (l *Loopback) Up(cfg Config) (Status, error) {
	l.status = Status{}
	negotiated := model.PQOff
	switch cfg.PQRequirement {
	case model.PQRequired:
		if !l.PQAvailable {
			return Status{}, ErrPQRequiredUnavailable // fail closed
		}
		negotiated = model.PQRequired // hybrid-pq active
	case model.PQPreferred:
		if l.PQAvailable {
			negotiated = model.PQRequired
		} else {
			negotiated = model.PQOff // permitted downgrade, but recorded
		}
	default:
		negotiated = model.PQOff
	}
	l.status = Status{Up: true, GatewayID: cfg.GatewayID, NegotiatedPQ: negotiated}
	return l.status, nil
}

// Down tears the tunnel down.
func (l *Loopback) Down() error {
	l.status = Status{}
	return nil
}

// Status returns the last known status.
func (l *Loopback) Status() Status { return l.status }
