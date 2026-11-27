// Package authorizer calls an external OpenID AuthZEN 1.0 policy decision point.
// Contract: docs/reference/gateway.md#authorizer-openid-authzen-10.
package authorizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/plat5dev/operator/internal/httpx"
	"github.com/plat5dev/operator/internal/metrics"
)

const (
	DefaultTimeout = 2 * time.Second
	maxRespBytes   = 1 << 20
	maxReasonChars = 512
)

// ErrUnavailable means the PDP could not be reached or answered something invalid.
// The gateway fails closed with 503.
var ErrUnavailable = errors.New("authorizer unavailable")

type Subject struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Action struct {
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Resource struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Request struct {
	Subject  Subject        `json:"subject"`
	Action   Action         `json:"action"`
	Resource Resource       `json:"resource"`
	Context  map[string]any `json:"context,omitempty"`
}

type BatchItem struct {
	Action   Action   `json:"action"`
	Resource Resource `json:"resource"`
}

type Result struct {
	Allowed bool
	Reason  string
}

type Config struct {
	URL     string
	Timeout time.Duration
	Token   string
	Metrics *metrics.Registry
	// HTTPClient is optional (mainly for tests). Redirects are never followed.
	HTTPClient *http.Client
}

type Client struct {
	baseURL  string
	timeout  time.Duration
	token    string
	client   *http.Client
	evals    *metrics.Counter
	duration *metrics.Histogram
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: httpx.NewTransport(timeout),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	reg := cfg.Metrics
	if reg == nil {
		reg = metrics.NewRegistry()
	}
	c := &Client{
		baseURL: strings.TrimRight(cfg.URL, "/"),
		timeout: timeout,
		token:   cfg.Token,
		client:  httpClient,
		evals: reg.Counter("operator_authorizer_evaluations_total",
			"AuthZEN evaluations, by decision.", "decision"),
		duration: reg.Histogram("operator_authorizer_duration_seconds",
			"Time spent calling the AuthZEN authorizer, by decision.",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}, "decision"),
	}
	for _, d := range []string{"allow", "deny", "error"} {
		c.evals.Add(0, d)
	}
	return c
}

// Evaluate calls POST {url}/access/v1/evaluation live (no caching).
func (c *Client) Evaluate(ctx context.Context, req Request) (Result, error) {
	start := time.Now()
	body, err := json.Marshal(req)
	if err != nil {
		c.observe("error", start)
		return Result{}, fmt.Errorf("%w: marshal: %v", ErrUnavailable, err)
	}
	raw, err := c.post(ctx, "/access/v1/evaluation", reqIDFromContext(req.Context), body)
	if err != nil {
		c.observe("error", start)
		return Result{}, err
	}
	var resp evalResponse
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Decision == nil {
		c.observe("error", start)
		return Result{}, fmt.Errorf("%w: invalid evaluation response", ErrUnavailable)
	}
	res := Result{Allowed: *resp.Decision, Reason: extractReason(resp.Context)}
	if res.Allowed {
		c.observe("allow", start)
	} else {
		c.observe("deny", start)
	}
	return res, nil
}

// EvaluateBatch calls POST {url}/access/v1/evaluations with execute_all semantics.
func (c *Client) EvaluateBatch(ctx context.Context, subject Subject, reqCtx map[string]any, items []BatchItem) ([]Result, error) {
	if len(items) == 0 {
		return nil, nil
	}
	start := time.Now()
	payload := struct {
		Subject     Subject        `json:"subject"`
		Context     map[string]any `json:"context,omitempty"`
		Options     map[string]any `json:"options"`
		Evaluations []BatchItem    `json:"evaluations"`
	}{
		Subject:     subject,
		Context:     reqCtx,
		Options:     map[string]any{"evaluations_semantic": "execute_all"},
		Evaluations: items,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		c.observe("error", start)
		return nil, fmt.Errorf("%w: marshal batch: %v", ErrUnavailable, err)
	}
	raw, err := c.post(ctx, "/access/v1/evaluations", reqIDFromContext(reqCtx), body)
	if err != nil {
		c.observe("error", start)
		return nil, err
	}
	var resp struct {
		Evaluations []evalResponse `json:"evaluations"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Evaluations) != len(items) {
		c.observe("error", start)
		return nil, fmt.Errorf("%w: invalid batch evaluation response", ErrUnavailable)
	}
	out := make([]Result, len(items))
	for i, ev := range resp.Evaluations {
		if ev.Decision == nil {
			c.observe("error", start)
			return nil, fmt.Errorf("%w: missing decision at index %d", ErrUnavailable, i)
		}
		out[i] = Result{Allowed: *ev.Decision, Reason: extractReason(ev.Context)}
	}
	c.observe("allow", start)
	return out, nil
}

type evalResponse struct {
	Decision *bool          `json:"decision"`
	Context  map[string]any `json:"context"`
}

func (c *Client) post(ctx context.Context, path, requestID string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxRespBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", ErrUnavailable, err)
	}
	return raw, nil
}

func (c *Client) observe(decision string, start time.Time) {
	c.evals.Inc(decision)
	c.duration.Observe(time.Since(start).Seconds(), decision)
}

func reqIDFromContext(m map[string]any) string {
	if m == nil {
		return ""
	}
	s, _ := m["request_id"].(string)
	return s
}

func extractReason(ctx map[string]any) string {
	if ctx == nil {
		return ""
	}
	raw, ok := ctx["reason"].(string)
	if !ok {
		return ""
	}
	s := strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(raw, ""), "\x00", ""))
	if utf8.RuneCountInString(s) > maxReasonChars {
		s = string([]rune(s)[:maxReasonChars])
	}
	return s
}
