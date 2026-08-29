// Package otel is the production adapter for the gim.Sink port. It forwards the
// ALREADY-SANITISED events emitted by the GIM privacy boundary to an
// OpenTelemetry Collector (third_party/manifest.yaml: otel-collector 0.109.0).
//
// Ordering guarantee: this sink only ever receives allowlisted fields because
// gim.Boundary sanitises before calling Consume (see telemetry-prohibition-list).
// Delivery is best-effort and synchronous with a strict timeout. Callers that
// cannot tolerate that bound must place an explicitly bounded queue upstream.
package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantmindSSI/haechi/services/gim"
)

// HTTPSink posts sanitised events as JSON to a collector receiver endpoint.
type HTTPSink struct {
	endpoint string
	http     *http.Client
	onError  func(error)
}

var _ gim.Sink = (*HTTPSink)(nil)

// Option configures the sink.
type Option func(*HTTPSink)

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(s *HTTPSink) { s.http = h } }

// WithErrorHandler observes delivery errors (metrics/log). Never receives event
// data, only the error.
func WithErrorHandler(f func(error)) Option { return func(s *HTTPSink) { s.onError = f } }

// NewHTTP builds a sink posting to endpoint (an OTel Collector receiver URL).
func NewHTTP(endpoint string, opts ...Option) *HTTPSink {
	s := &HTTPSink{endpoint: endpoint, http: &http.Client{Timeout: 2 * time.Second}}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Consume forwards one sanitised event with a two-second deadline.
func (s *HTTPSink) Consume(e gim.Event) {
	body, err := json.Marshal(e)
	if err != nil {
		s.report(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		s.report(err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		s.report(err)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		s.report(fmt.Errorf("otel: collector returned status %d", resp.StatusCode))
	}
	if err := resp.Body.Close(); err != nil {
		s.report(fmt.Errorf("otel: close response: %w", err))
	}
}

func (s *HTTPSink) report(err error) {
	if s.onError != nil {
		s.onError(err)
	}
}
