package gim

import "testing"

// TestProhibitedFieldsDropped is the required release gate (§12.3): prove no
// prohibited field can cross the boundary into the sink.
func TestProhibitedFieldsDropped(t *testing.T) {
	sink := &CollectingSink{}
	b := New(sink)

	res := b.Submit(Event{
		// allowed
		"gateway_id":    "gw-eu-1",
		"region":        "eu-west",
		"service_class": "general",
		"pq_mode":       "hybrid-pq",
		"outcome":       "success",
		"epoch":         uint64(42),
		// prohibited; the complete event must be dropped
		"payload":          []byte("secret bytes"),
		"dns_query":        "bank.example.com",
		"user_id":          "alice@acme",
		"src_ip":           "203.0.113.5",
		"capability_nonce": "abcd",
		"private_key":      "-----BEGIN...",
	})

	if !res.Rejected || res.Accepted != nil {
		t.Fatalf("contaminated event was not rejected: %+v", res)
	}
	if len(res.ProhibitedHits) != 6 {
		t.Fatalf("want 6 prohibited hits, got %d (%v)", len(res.ProhibitedHits), res.ProhibitedHits)
	}
	if len(sink.Events) != 0 {
		t.Fatalf("contaminated event reached sink: %+v", sink.Events)
	}
}

func TestUnknownFieldsDroppedByDefault(t *testing.T) {
	b := New(nil)
	res := b.Submit(Event{
		"gateway_id":      "gw-1",
		"some_new_field":  "value", // unknown, not on either list
		"another_unknown": 123,
	})
	if !res.Rejected || res.Accepted != nil {
		t.Fatalf("unknown field did not reject complete event: %+v", res)
	}
}

func TestAllowlistedFieldRejectsSensitiveValueShape(t *testing.T) {
	sink := &CollectingSink{}
	b := New(sink)
	res := b.Submit(Event{"gateway_id": "alice@example.com", "count": 1})
	if !res.Rejected || len(res.InvalidFields) != 1 || res.InvalidFields[0] != "gateway_id" {
		t.Fatalf("sensitive value shape was not rejected: %+v", res)
	}
	if len(sink.Events) != 0 {
		t.Fatal("invalid event reached sink")
	}
}

func TestAllowlistedEventPassesThrough(t *testing.T) {
	b := New(nil)
	res := b.Submit(Event{
		"gateway_id":     "gw-1",
		"outcome":        "failure",
		"latency_bucket": "50-100ms",
		"count":          3,
	})
	if len(res.DroppedKeys) != 0 {
		t.Fatalf("valid event dropped keys: %v", res.DroppedKeys)
	}
	if len(res.Accepted) != 4 {
		t.Fatalf("want 4 accepted, got %d", len(res.Accepted))
	}
}
