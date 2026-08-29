// Package events is the asynchronous control-event PORT (Integration Document
// §6.2 "emits a versioned event", §7 "asynchronous events use versioned schemas
// over NATS"). The production adapter (adapters/nats) publishes to NATS subjects;
// MemoryBus is the dev adapter.
//
// Subjects and payload schemas are the single source of truth in api/events/.
package events

import (
	"context"
	"sync"
	"time"
)

// Subjects are the versioned NATS subjects. Schemas live in api/events/.
const (
	SubjectDeviceEnrolled  = "haechi.device.enrolled.v1"
	SubjectDeviceRevoked   = "haechi.device.revoked.v1"
	SubjectGatewayHealth   = "haechi.gateway.health.v1"
	SubjectPolicyPublished = "haechi.policy.published.v1"
)

// Event is a versioned control event. Data is the JSON-encoded payload matching
// the subject's schema.
type Event struct {
	Subject string
	Data    []byte
	Time    time.Time
}

// Publisher publishes control events.
type Publisher interface {
	Publish(ctx context.Context, ev Event) error
}

// Handler consumes an event (dev bus).
type Handler func(Event)

// MemoryBus is an in-process pub/sub dev adapter with synchronous delivery.
// Production uses NATS with authenticated subjects and bounded retention.
type MemoryBus struct {
	mu   sync.RWMutex
	subs map[string][]Handler
	log  []Event
}

// NewMemoryBus builds an empty bus.
func NewMemoryBus() *MemoryBus { return &MemoryBus{subs: map[string][]Handler{}} }

// Publish delivers to all subscribers of the subject and records the event.
func (b *MemoryBus) Publish(_ context.Context, ev Event) error {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	b.mu.Lock()
	b.log = append(b.log, ev)
	handlers := append([]Handler(nil), b.subs[ev.Subject]...)
	b.mu.Unlock()
	for _, h := range handlers {
		h(ev)
	}
	return nil
}

// Subscribe registers a handler for a subject.
func (b *MemoryBus) Subscribe(subject string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[subject] = append(b.subs[subject], h)
}

// Count returns how many events for a subject have been published (tests).
func (b *MemoryBus) Count(subject string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, e := range b.log {
		if e.Subject == subject {
			n++
		}
	}
	return n
}
