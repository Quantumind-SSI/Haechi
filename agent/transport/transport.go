// Package transport selects between direct and protected endpoint transports
// (ADR-006). Protected transport (e.g. an isolated Xray sidecar) is opt-in,
// disabled unless policy allows it, and never weakens authentication; it only
// changes how the encrypted tunnel bytes traverse a restrictive network.
package transport

import (
	"errors"

	"github.com/QuantmindSSI/haechi/internal/model"
)

// ErrNoTransport means no permitted transport could be selected. Fail closed.
var ErrNoTransport = errors.New("transport: no permitted transport available")

// Select chooses a transport. Direct is always preferred. Protected is used
// only if direct is unavailable AND policy explicitly allows it.
func Select(allowed []model.Transport, directAvailable bool) (model.Transport, error) {
	allow := map[model.Transport]bool{}
	for _, a := range allowed {
		allow[a] = true
	}
	if directAvailable && allow[model.TransportDirect] {
		return model.TransportDirect, nil
	}
	if allow[model.TransportProtected] {
		return model.TransportProtected, nil
	}
	return "", ErrNoTransport
}
