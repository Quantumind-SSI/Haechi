// Package gim implements the Global Intelligence Mesh privacy boundary
// (Integration Document §6.6, §12.3; dev-docs/threat-model/telemetry-prohibition-list.md).
//
// The boundary is DEFAULT-DENY: only explicitly allowlisted, coarse operational
// fields may cross it. Any event containing a prohibited or unknown field is
// dropped and only the fact of a drop (plus which keys) is recorded, never the
// prohibited value. This is the enforcement point that keeps payloads, DNS
// content, durable identifiers, addresses and secrets out of analytics.
package gim

import (
	"regexp"
	"sort"
)

// allowedFields is the complete allowlist. Keys not present here are dropped.
var allowedFields = map[string]bool{
	"gateway_id":     true,
	"region":         true,
	"service_class":  true,
	"pq_mode":        true,
	"outcome":        true,
	"latency_bucket": true,
	"epoch":          true,
	"count":          true,
}

// prohibited names fields that must NEVER appear, used to raise louder alerts
// when a producer bug attempts to emit them. Any non-allowlisted key is dropped
// regardless; this set exists for explicit, testable evidence.
var prohibited = map[string]bool{
	"payload":          true,
	"body":             true,
	"dns_query":        true,
	"dns_answer":       true,
	"user_id":          true,
	"email":            true,
	"idp_subject":      true,
	"account_id":       true,
	"src_ip":           true,
	"client_ip":        true,
	"mac":              true,
	"token":            true,
	"capability_nonce": true,
	"private_key":      true,
	"psk":              true,
	"cookie":           true,
	"authorization":    true,
}

// Event is a flat operational measurement.
type Event map[string]any

// Result reports what the boundary did with an event.
type Result struct {
	// Accepted is populated only when the complete event passes validation.
	Accepted Event
	// DroppedKeys lists non-allowlisted keys that caused rejection.
	DroppedKeys []string
	// InvalidFields lists allowlisted keys whose values failed validation.
	InvalidFields []string
	// ProhibitedHits lists rejected keys on the explicit prohibition list.
	ProhibitedHits []string
	Rejected       bool
}

// Sink consumes sanitised events. It only ever receives allowlisted fields.
type Sink interface {
	Consume(Event)
}

// Boundary validates events and forwards only complete, valid events to a sink.
type Boundary struct {
	sink Sink
}

// New builds a boundary over a sink. A nil sink discards accepted events.
func New(sink Sink) *Boundary { return &Boundary{sink: sink} }

var (
	identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	latencyPattern    = regexp.MustCompile(`^(?:[0-9]{1,5}-[0-9]{1,5}ms|[0-9]{1,5}ms\+)$`)
)

// Submit validates ev against the allowlist. Any unknown, prohibited, or
// malformed field rejects the complete event.
func (b *Boundary) Submit(ev Event) Result {
	accepted := Event{}
	var dropped, invalid, prohibitedHits []string
	for k, v := range ev {
		if !allowedFields[k] {
			dropped = append(dropped, k)
			if prohibited[k] {
				prohibitedHits = append(prohibitedHits, k)
			}
			continue
		}
		if !validValue(k, v) {
			invalid = append(invalid, k)
			continue
		}
		accepted[k] = v
	}
	sort.Strings(dropped)
	sort.Strings(invalid)
	sort.Strings(prohibitedHits)
	if len(dropped) > 0 || len(invalid) > 0 {
		return Result{
			DroppedKeys: dropped, InvalidFields: invalid,
			ProhibitedHits: prohibitedHits, Rejected: true,
		}
	}
	if b.sink != nil {
		b.sink.Consume(accepted)
	}
	return Result{Accepted: accepted}
}

func validValue(field string, value any) bool {
	switch field {
	case "gateway_id", "region", "service_class":
		text, ok := value.(string)
		return ok && identifierPattern.MatchString(text)
	case "pq_mode":
		text, ok := value.(string)
		return ok && (text == "hybrid-pq" || text == "classical")
	case "outcome":
		text, ok := value.(string)
		return ok && (text == "success" || text == "failure" || text == "denied")
	case "latency_bucket":
		text, ok := value.(string)
		return ok && latencyPattern.MatchString(text)
	case "epoch", "count":
		return nonNegativeInteger(value)
	default:
		return false
	}
}

func nonNegativeInteger(value any) bool {
	switch number := value.(type) {
	case int:
		return number >= 0
	case int64:
		return number >= 0
	case uint:
		return true
	case uint64:
		return true
	case float64:
		return number >= 0 && number <= 1<<53 && number == float64(uint64(number))
	default:
		return false
	}
}

// CollectingSink records everything it receives (tests / local dev).
type CollectingSink struct{ Events []Event }

// Consume implements Sink.
func (c *CollectingSink) Consume(e Event) { c.Events = append(c.Events, e) }
