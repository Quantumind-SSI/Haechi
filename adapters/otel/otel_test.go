package otel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantmindSSI/haechi/services/gim"
)

func TestHTTPSinkPostsValidatedEvent(t *testing.T) {
	var (
		mu   sync.Mutex
		body []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	sink := NewHTTP(srv.URL, WithHTTPClient(srv.Client()))

	b := gim.New(sink)
	b.Submit(gim.Event{
		"gateway_id": "gw-1",
		"outcome":    "success",
	})

	mu.Lock()
	got := body
	mu.Unlock()
	var ev map[string]any
	if err := json.Unmarshal(got, &ev); err != nil {
		t.Fatalf("collector received non-JSON: %v", err)
	}
	if ev["gateway_id"] != "gw-1" {
		t.Fatalf("expected gateway_id, got %+v", ev)
	}
}

func TestHTTPSinkReportsCollectorFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var reported error
	sink := NewHTTP(srv.URL, WithHTTPClient(srv.Client()), WithErrorHandler(func(err error) {
		reported = err
	}))
	sink.Consume(gim.Event{"gateway_id": "gw-1"})
	if reported == nil {
		t.Fatal("collector failure was not reported")
	}
}
