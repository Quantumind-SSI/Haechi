// Package opa is the production adapter for the control-api authorisation port,
// wiring Open Policy Agent (third_party/manifest.yaml: opa 0.70.0) through its
// stable Data REST API. It is the ONLY place that talks to OPA.
//
// Decision document contract (single source of truth: deploy/opa/policy/authz.rego):
//
//	POST {base}/v1/data/haechi/authz/decision
//	{ "input": <AuthzInput> }  ->  { "result": { "allow": bool, "reason": string } }
//
// Fail closed: any transport error, non-200 status, or missing result denies.
package opa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	controlapi "github.com/QuantmindSSI/haechi/services/control-api"
)

// Client queries an OPA server. It satisfies controlapi.PolicyEngine.
type Client struct {
	baseURL string
	docPath string // OPA data path, default "haechi/authz/decision"
	http    *http.Client
}

var _ controlapi.PolicyEngine = (*Client)(nil)

// Option configures the client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (timeouts, mTLS to OPA).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithDocumentPath overrides the OPA data document path.
func WithDocumentPath(p string) Option { return func(c *Client) { c.docPath = strings.Trim(p, "/") } }

// New builds an OPA adapter pointing at baseURL (e.g. http://opa:8181).
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		docPath: "haechi/authz/decision",
		http:    &http.Client{Timeout: 3 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type opaRequest struct {
	Input controlapi.AuthzInput `json:"input"`
}

type opaResponse struct {
	Result *struct {
		Allow  bool   `json:"allow"`
		Reason string `json:"reason"`
	} `json:"result"`
}

// Authorize sends the input to OPA and maps the decision document. It fails
// closed on any error or absent result.
func (c *Client) Authorize(ctx context.Context, in controlapi.AuthzInput) (controlapi.Decision, error) {
	body, err := json.Marshal(opaRequest{Input: in})
	if err != nil {
		return controlapi.Decision{}, err
	}
	url := c.baseURL + "/v1/data/" + c.docPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return controlapi.Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return controlapi.Decision{}, fmt.Errorf("opa: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return controlapi.Decision{}, fmt.Errorf("opa: status %d", resp.StatusCode)
	}
	var out opaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return controlapi.Decision{}, fmt.Errorf("opa: decode: %w", err)
	}
	if out.Result == nil {
		// No matching rule -> deny by default (definitive, not an error).
		return controlapi.Decision{Allow: false, Reason: "opa: deny-by-default (no decision)"}, nil
	}
	return controlapi.Decision{Allow: out.Result.Allow, Reason: out.Result.Reason}, nil
}
