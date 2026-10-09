// Package audit writes the staff audit log from the gateway. Contract: docs/audit.md.
// The intent is written before the gateway answers or forwards, and the client waits on it.
// The outcome follows in the background. Audit is on or off for the whole deployment.
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/plat5dev/operator/internal/metrics"
)

// DetailsHeader is where a service says what changed. The gateway stores it as sent and
// strips it, from requests and responses, on every route.
const DetailsHeader = "X-Plat5-Audit-Details"

const (
	MaxDetailsBytes   = 4096
	MaxUserAgentChars = 512
	MaxPathChars      = 2048

	intentAttempts       = 3
	intentAttemptTimeout = 500 * time.Millisecond
	// The client waits on the intent, so every attempt fits in this.
	intentBudget = time.Second

	outcomeQueue          = 10_000
	outcomeInFlight       = 64
	outcomeAttemptTimeout = 2 * time.Second
)

// Wait before each outcome retry. About five minutes in all, then the event stays pending.
var outcomeBackoff = []time.Duration{
	200 * time.Millisecond,
	time.Second,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
	time.Minute,
	time.Minute,
	time.Minute,
	time.Minute,
}

// ErrIntentNotWritten means the gateway did not see the intent written. The caller answers
// 503 and does not forward.
var ErrIntentNotWritten = errors.New("audit intent not written")

type Actor struct {
	Issuer     string  `json:"issuer"`
	OperatorID string  `json:"operator_id"`
	Email      *string `json:"email"`
	ClientID   *string `json:"client_id"`
}

// Intent is everything the gateway knows before it answers or forwards. RequestID is in the URL.
type Intent struct {
	RequestID  string            `json:"-"`
	OccurredAt string            `json:"occurred_at"`
	Actor      Actor             `json:"actor"`
	Upstream   *string           `json:"upstream"`
	Method     string            `json:"method"`
	Route      *string           `json:"route"`
	Params     map[string]string `json:"params"`
	Path       *string           `json:"path"`
	IP         string            `json:"ip"`
	UserAgent  *string           `json:"user_agent"`
}

type OutcomeKind string

const (
	// Rejected means the gateway answered without calling the service.
	Rejected OutcomeKind = "rejected"
	// Responded means the service answered.
	Responded OutcomeKind = "responded"
	// NoResponse means the gateway called the service and got no answer.
	NoResponse OutcomeKind = "no_response"
)

type Outcome struct {
	Outcome OutcomeKind `json:"outcome"`
	// Status is what the gateway answered with. Nil when the client was gone first.
	Status        *int  `json:"status"`
	ResponseBytes int64 `json:"response_bytes"`
	// Decision is reserved for authz (slice 3). Always null.
	Decision json.RawMessage `json:"decision"`
	Details  json.RawMessage `json:"details"`
}

// State is what the gateway learns about a request between its intent and its outcome.
type State struct {
	// Forwarded means the gateway handed the request to the upstream.
	Forwarded bool
	// UpstreamStatus is set when the upstream answers.
	UpstreamStatus int
	Details        json.RawMessage
	// ClientGone means the client left before the gateway could answer.
	ClientGone bool
}

type stateKey struct{}

// WithState carries s to the proxy hooks, which see only the request.
func WithState(ctx context.Context, s *State) context.Context {
	return context.WithValue(ctx, stateKey{}, s)
}

// StateFrom is the request's State, or nil when audit is off.
func StateFrom(ctx context.Context) *State {
	s, _ := ctx.Value(stateKey{}).(*State)
	return s
}

// OutcomeFor is how the request ended. written is the status the gateway wrote, 0 for none.
func OutcomeFor(s *State, written int, responseBytes int64) Outcome {
	out := Outcome{ResponseBytes: responseBytes}
	status := func(code int) *int {
		if code == 0 {
			return nil
		}
		return &code
	}
	switch {
	case s.UpstreamStatus != 0:
		out.Outcome = Responded
		out.Status = status(s.UpstreamStatus)
		out.Details = s.Details
	case s.Forwarded:
		out.Outcome = NoResponse
		if !s.ClientGone {
			out.Status = status(written)
		}
	default:
		out.Outcome = Rejected
		out.Status = status(written)
	}
	return out
}

// ParseDetails is the details header as one JSON object of visible ASCII within the cap.
// ok is false when the header is there but unusable; the event then records null.
func ParseDetails(values []string) (details json.RawMessage, ok bool) {
	if len(values) != 1 {
		return nil, false
	}
	v := values[0]
	if len(v) > MaxDetailsBytes {
		return nil, false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return nil, false
		}
	}
	v = strings.TrimSpace(v)
	// Postgres cannot store a NUL in jsonb. An event with it would never get its outcome.
	if !strings.HasPrefix(v, "{") || strings.Contains(v, `\u0000`) || !json.Valid([]byte(v)) {
		return nil, false
	}
	return json.RawMessage(v), true
}

// Text makes s storable: invalid UTF-8 and NUL become U+FFFD.
func Text(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.ReplaceAll(s, "\x00", "�")
}

// UserAgent is the header cut to MaxUserAgentChars characters, or nil when absent.
func UserAgent(s string) *string {
	if s == "" {
		return nil
	}
	s = Text(s)
	if utf8.RuneCountInString(s) > MaxUserAgentChars {
		s = string([]rune(s)[:MaxUserAgentChars])
	}
	return &s
}

// Path is an unmatched request's raw path, cut to MaxPathChars.
func Path(escaped string) string {
	s := Text(escaped)
	if utf8.RuneCountInString(s) > MaxPathChars {
		s = string([]rune(s)[:MaxPathChars])
	}
	return s
}

// ClientIP is the TCP peer's address. X-Forwarded-For is not read.
func ClientIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// Timestamp is RFC 3339, UTC, milliseconds: 2026-10-09T18:30:00.123Z.
func Timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

type Config struct {
	// URL is operator-audit's internal base URL.
	URL string
	// Token is sent as a bearer on every write.
	Token   string
	Log     *slog.Logger
	Metrics *metrics.Registry
	// Client is for tests. Default is a plain client; each write sets its own deadline.
	Client *http.Client
}

// Writer sends intents and outcomes to operator-audit.
type Writer struct {
	url    string
	token  string
	client *http.Client
	log    *slog.Logger

	queue   chan queued
	waiting atomic.Int64
	mu      sync.RWMutex
	closed  bool
	// ctx ends when shutdown runs out of time. It aborts outcome retries.
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	backoff []time.Duration

	writes   *metrics.Counter
	duration *metrics.Histogram
}

type queued struct {
	requestID string
	outcome   Outcome
}

// New starts the outcome sender. Call Close on shutdown.
func New(cfg Config) *Writer {
	client := cfg.Client
	if client == nil {
		client = &http.Client{}
	}
	reg := cfg.Metrics
	if reg == nil {
		reg = metrics.NewRegistry()
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Writer{
		url:     strings.TrimRight(cfg.URL, "/"),
		token:   cfg.Token,
		client:  client,
		log:     cfg.Log,
		queue:   make(chan queued, outcomeQueue),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		backoff: outcomeBackoff,
		writes: reg.Counter("operator_audit_writes_total",
			"Audit writes to operator-audit, by write and result.", "write", "result"),
		duration: reg.Histogram("operator_audit_intent_duration_seconds",
			"Time the client waited on the audit intent, by result.",
			[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}, "result"),
	}
	for _, r := range []string{"ok", "failed"} {
		w.writes.Add(0, "intent", r)
	}
	for _, r := range []string{"ok", "failed", "dropped", "not_found"} {
		w.writes.Add(0, "outcome", r)
	}
	reg.GaugeFunc("operator_audit_outcome_queue", "Audit outcomes waiting to be sent.",
		func() float64 { return float64(w.waiting.Load()) })
	go w.run()
	return w
}

func (w *Writer) eventURL(requestID string) string {
	return w.url + "/internal/events/" + url.PathEscape(requestID)
}

// WriteIntent writes the intent, retrying within the budget. A client that leaves does
// not cancel it. Err means the gateway did not see it written.
func (w *Writer) WriteIntent(ctx context.Context, in *Intent) error {
	ctx = context.WithoutCancel(ctx)
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIntentNotWritten, err)
	}
	start := time.Now()
	deadline := start.Add(intentBudget)
	var last string
	for range intentAttempts {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		status, err := w.send(ctx, http.MethodPut, in.RequestID, body, min(left, intentAttemptTimeout))
		if err == nil && (status == http.StatusOK || status == http.StatusCreated) {
			w.writes.Inc("intent", "ok")
			w.duration.Observe(time.Since(start).Seconds(), "ok")
			return nil
		}
		if err != nil {
			last = err.Error()
			continue
		}
		last = fmt.Sprintf("operator-audit returned status %d", status)
		// A 4xx is the same on every try: a bug or a token mismatch.
		if status >= 400 && status < 500 {
			break
		}
	}
	w.log.Error("audit intent not written; not forwarding", "request_id", in.RequestID, "err", last)
	w.writes.Inc("intent", "failed")
	w.duration.Observe(time.Since(start).Seconds(), "failed")
	return ErrIntentNotWritten
}

// SendOutcome queues the outcome. It never waits. A full queue drops it and the event
// stays pending.
func (w *Writer) SendOutcome(requestID string, out Outcome) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		w.log.Warn("audit outcome after shutdown; event stays pending", "request_id", requestID)
		w.writes.Inc("outcome", "dropped")
		return
	}
	w.waiting.Add(1)
	select {
	case w.queue <- queued{requestID: requestID, outcome: out}:
	default:
		w.waiting.Add(-1)
		w.log.Warn("audit outcome queue full; event stays pending", "request_id", requestID)
		w.writes.Inc("outcome", "dropped")
	}
}

// Close stops taking outcomes and sends what is queued until ctx ends. What is left
// then stays pending.
func (w *Writer) Close(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
	}
	left := w.waiting.Load()
	w.cancel()
	<-w.done
	return fmt.Errorf("%d audit outcomes not sent at shutdown; their events stay pending", left)
}

func (w *Writer) run() {
	defer close(w.done)
	slots := make(chan struct{}, outcomeInFlight)
	var wg sync.WaitGroup
	for q := range w.queue {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.writeOutcome(q)
			w.waiting.Add(-1)
			<-slots
		}()
	}
	wg.Wait()
	w.cancel()
}

// writeOutcome PATCHes until operator-audit takes it, says there is no intent, or the
// backoff runs out. An outcome applies once, so a retry after a lost reply is harmless.
func (w *Writer) writeOutcome(q queued) {
	body, err := json.Marshal(q.outcome)
	if err != nil {
		w.log.Error("audit outcome not encoded; event stays pending", "request_id", q.requestID, "err", err)
		w.writes.Inc("outcome", "failed")
		return
	}
	waits := w.backoff
	for {
		status, err := w.send(w.ctx, http.MethodPatch, q.requestID, body, outcomeAttemptTimeout)
		switch {
		case err == nil && status >= 200 && status < 300:
			w.writes.Inc("outcome", "ok")
			return
		case err == nil && status == http.StatusNotFound:
			// The intent never landed (the 503 path). Nothing to finish.
			w.writes.Inc("outcome", "not_found")
			return
		case err == nil && status >= 400 && status < 500:
			w.log.Error("operator-audit refused the outcome; event stays pending", "request_id", q.requestID, "status", status)
			w.writes.Inc("outcome", "failed")
			return
		}
		if w.ctx.Err() != nil {
			w.log.Error("audit outcome not sent before shutdown; event stays pending", "request_id", q.requestID)
			w.writes.Inc("outcome", "dropped")
			return
		}
		if len(waits) == 0 {
			w.log.Error("audit outcome not written; event stays pending", "request_id", q.requestID, "status", status, "err", err)
			w.writes.Inc("outcome", "failed")
			return
		}
		t := time.NewTimer(waits[0])
		select {
		case <-t.C:
		case <-w.ctx.Done():
			t.Stop()
		}
		waits = waits[1:]
	}
}

func (w *Writer) send(ctx context.Context, method, requestID string, body []byte, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, w.eventURL(requestID), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("X-Request-ID", requestID)
	res, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()
	return res.StatusCode, nil
}
