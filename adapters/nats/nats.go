// Package nats is the production adapter for the events.Publisher port, wiring
// NATS (third_party/manifest.yaml: nats 2.10.20). It is the ONLY place that
// talks to NATS.
//
// It depends on a minimal Conn interface satisfied by *nats.Conn from
// github.com/nats-io/nats.go, which the service entrypoint injects under the
// `haechi_nats` build. This keeps the default offline build free of the client
// dependency while making the wiring explicit and testable.
package nats

import (
	"context"

	"github.com/QuantmindSSI/haechi/internal/events"
)

// Conn is the subset of *nats.Conn used by Haechi. The real client satisfies it:
//
//	nc, _ := nats.Connect(url, nats.UserCredentials(...))
//	pub := natsadapter.New(nc, "haechi")
type Conn interface {
	Publish(subject string, data []byte) error
}

// Publisher forwards Haechi events to NATS subjects.
type Publisher struct {
	conn Conn
}

var _ events.Publisher = (*Publisher)(nil)

// New wraps a NATS connection.
func New(conn Conn) *Publisher { return &Publisher{conn: conn} }

// Publish sends the event to its NATS subject.
func (p *Publisher) Publish(_ context.Context, ev events.Event) error {
	return p.conn.Publish(ev.Subject, ev.Data)
}
