package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plat5dev/operator/internal/apierr"
)

// memStore keeps events in memory with the store's write rules.
type memStore struct {
	events  map[string]*Event
	listErr error
	filter  Filter
}

func (m *memStore) CreateIntent(_ context.Context, in *Intent) (bool, error) {
	if m.events == nil {
		m.events = map[string]*Event{}
	}
	if _, ok := m.events[in.RequestID]; ok {
		return false, nil
	}
	m.events[in.RequestID] = &Event{ID: newID(in.OccurredAt), Intent: *in, Outcome: OutcomePending}
	return true, nil
}

func (m *memStore) ApplyOutcome(_ context.Context, requestID string, out *Outcome) (bool, error) {
	e, ok := m.events[requestID]
	if !ok {
		return false, ErrNotFound
	}
	if e.Outcome != OutcomePending {
		return false, nil
	}
	e.Outcome, e.Status, e.ResponseBytes, e.Details = out.Outcome, out.Status, &out.ResponseBytes, out.Details
	return true, nil
}

func (m *memStore) List(_ context.Context, f Filter) ([]*Event, bool, error) {
	m.filter = f
	var out []*Event
	for _, e := range m.events {
		out = append(out, e)
	}
	return out, false, m.listErr
}

func mux(s store) http.Handler {
	h := NewHandler(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.now = func() time.Time { return now }
	m := http.NewServeMux()
	h.MountInternal(m, "tok")
	h.MountPublic(m)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.ServeHTTP(w, r.WithContext(apierr.WithRequestID(r.Context(), "req")))
	})
}

func call(h http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func code(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q: %v", w.Body, err)
	}
	return e.Error.Code
}

func TestInternalNeedsTheToken(t *testing.T) {
	h := mux(&memStore{})
	for _, token := range []string{"", "nope"} {
		w := call(h, "PUT", "/internal/events/r1", string(intentJSON(t, nil)), token)
		if w.Code != 401 || code(t, w) != "UNAUTHORIZED" {
			t.Fatalf("token %q: %d", token, w.Code)
		}
	}
}

func TestIntentThenOutcome(t *testing.T) {
	s := &memStore{}
	h := mux(s)
	body := string(intentJSON(t, nil))
	if w := call(h, "PUT", "/internal/events/r1", body, "tok"); w.Code != 201 {
		t.Fatalf("intent %d %s", w.Code, w.Body)
	}
	if w := call(h, "PUT", "/internal/events/r1", body, "tok"); w.Code != 200 {
		t.Fatalf("retried intent %d", w.Code)
	}
	if w := call(h, "PUT", "/internal/events/r2", `{"method":"GET"}`, "tok"); w.Code != 422 || code(t, w) != "VALIDATION_ERROR" {
		t.Fatalf("bad intent %d", w.Code)
	}

	outcome := `{"outcome":"responded","status":200,"response_bytes":3,"decision":null,"details":{"a":1}}`
	if w := call(h, "PATCH", "/internal/events/r1", outcome, "tok"); w.Code != 204 {
		t.Fatalf("outcome %d %s", w.Code, w.Body)
	}
	if w := call(h, "PATCH", "/internal/events/r1", `{"outcome":"rejected","status":503,"response_bytes":0}`, "tok"); w.Code != 204 {
		t.Fatalf("second outcome %d", w.Code)
	}
	if s.events["r1"].Outcome != OutcomeResponded {
		t.Fatal("a final event changed")
	}
	if w := call(h, "PATCH", "/internal/events/r9", outcome, "tok"); w.Code != 404 {
		t.Fatalf("no intent %d", w.Code)
	}
	if w := call(h, "PATCH", "/internal/events/r1", `{"outcome":"pending"}`, "tok"); w.Code != 422 {
		t.Fatalf("pending outcome %d", w.Code)
	}
}

func TestList(t *testing.T) {
	s := &memStore{}
	h := mux(s)
	call(h, "PUT", "/internal/events/r1", string(intentJSON(t, nil)), "tok")

	w := call(h, "GET", "/operator-audit-events?operator_id=op_1&limit=10", "", "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if s.filter.OperatorID != "op_1" || s.filter.Limit != 10 {
		t.Fatalf("filter %+v", s.filter)
	}
	var got struct {
		AuditEvents []map[string]any `json:"audit_events"`
		HasMore     *bool            `json:"has_more"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.AuditEvents) != 1 || got.HasMore == nil {
		t.Fatalf("%s", w.Body)
	}
	e := got.AuditEvents[0]
	if e["occurred_at"] != "2026-10-09T18:30:00.123Z" || e["outcome"] != "pending" {
		t.Fatalf("%v", e)
	}
	for _, k := range []string{"status", "response_bytes", "decision", "details", "path"} {
		if v, ok := e[k]; !ok || v != nil {
			t.Errorf("%s = %v, want null", k, v)
		}
	}
	if actor := e["actor"].(map[string]any); actor["email"] != nil || actor["client_id"] != "console" {
		t.Fatalf("actor %v", actor)
	}

	if w := call(h, "GET", "/operator-audit-events?organization_id=x", "", ""); w.Code != 422 {
		t.Fatalf("unknown filter %d", w.Code)
	}
	s.listErr = errors.New("down")
	if w := call(h, "GET", "/operator-audit-events", "", ""); w.Code != 500 || code(t, w) != "INTERNAL_ERROR" {
		t.Fatalf("store error %d", w.Code)
	}
}
