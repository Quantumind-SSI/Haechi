package nats

import (
	"context"
	"testing"

	"github.com/QuantmindSSI/haechi/internal/events"
)

// fakeConn stands in for *nats.Conn to prove the wire mapping.
type fakeConn struct {
	subject string
	data    []byte
}

func (f *fakeConn) Publish(subject string, data []byte) error {
	f.subject = subject
	f.data = data
	return nil
}

func TestPublishMapsToSubject(t *testing.T) {
	fc := &fakeConn{}
	p := New(fc)
	err := p.Publish(context.Background(), events.Event{
		Subject: events.SubjectDeviceEnrolled,
		Data:    []byte(`{"device_id":"dev_1"}`),
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if fc.subject != events.SubjectDeviceEnrolled {
		t.Fatalf("subject=%q", fc.subject)
	}
	if string(fc.data) != `{"device_id":"dev_1"}` {
		t.Fatalf("data=%q", fc.data)
	}
}
