package events

import (
	"context"
	"testing"
)

func TestMemoryBusPubSub(t *testing.T) {
	b := NewMemoryBus()
	var got int
	b.Subscribe(SubjectDeviceEnrolled, func(Event) { got++ })

	_ = b.Publish(context.Background(), Event{Subject: SubjectDeviceEnrolled, Data: []byte(`{"device_id":"dev_1"}`)})
	_ = b.Publish(context.Background(), Event{Subject: SubjectGatewayHealth, Data: []byte(`{}`)})

	if got != 1 {
		t.Fatalf("handler fired %d times, want 1", got)
	}
	if b.Count(SubjectDeviceEnrolled) != 1 {
		t.Fatalf("count=%d, want 1", b.Count(SubjectDeviceEnrolled))
	}
}
