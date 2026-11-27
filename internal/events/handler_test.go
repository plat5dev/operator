package events

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/plat5dev/operator/internal/apierr"
)

// memStore keeps events in memory with the store's write rules.
type memStore struct {
	events       map[string]*Event
	intentErr    error
	outcomeErr   error
	listErr      error
	exportErr    error
	exportEvents []*Event
	exportCursor int64
	exportMore   bool
	exportAfter  int64
	exportLimit  int
	filter       Filter
}

func (m *memStore) CreateIntent(_ context.Context, in *Intent) (bool, error) {
	if m.intentErr != nil {
		return false, m.intentErr
	}
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
	if m.outcomeErr != nil {
		return false, m.outcomeErr
	}
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

func (m *memStore) Export(_ context.Context, after int64, limit int) ([]*Event, int64, bool, error) {
	if m.exportErr != nil {
		return nil, 0, false, m.exportErr
	}
	m.exportAfter = after
	m.exportLimit = limit
	cursor := m.exportCursor
	if cursor == 0 {
		cursor = after
	}
	return m.exportEvents, cursor, m.exportMore, nil
}

func mux(s store) http.Handler {
	h := NewHandler(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.now = func() time.Time { return now }
	m := http.NewServeMux()
	h.MountInternal(m)
	h.MountPublic(m)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.ServeHTTP(w, r.WithContext(apierr.WithRequestID(r.Context(), "req")))
	})
}

func call(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
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

func TestIntentThenOutcome(t *testing.T) {
	s := &memStore{}
	h := mux(s)
	body := string(intentJSON(t, nil))
	if w := call(h, "PUT", "/internal/events/r1", body); w.Code != 201 {
		t.Fatalf("intent %d %s", w.Code, w.Body)
	}
	if w := call(h, "PUT", "/internal/events/r1", body); w.Code != 200 {
		t.Fatalf("retried intent %d", w.Code)
	}
	if w := call(h, "PUT", "/internal/events/r2", `{"method":"GET"}`); w.Code != 422 || code(t, w) != "VALIDATION_ERROR" {
		t.Fatalf("bad intent %d", w.Code)
	}

	outcome := `{"outcome":"responded","status":200,"response_bytes":3,"details":{"a":1},"created_id":null}`
	if w := call(h, "PATCH", "/internal/events/r1", outcome); w.Code != 204 {
		t.Fatalf("outcome %d %s", w.Code, w.Body)
	}
	if w := call(h, "PATCH", "/internal/events/r1", `{"outcome":"rejected","status":503,"response_bytes":0}`); w.Code != 204 {
		t.Fatalf("second outcome %d", w.Code)
	}
	if s.events["r1"].Outcome != OutcomeResponded {
		t.Fatal("a final event changed")
	}
	if w := call(h, "PATCH", "/internal/events/r9", outcome); w.Code != 404 {
		t.Fatalf("no intent %d", w.Code)
	}
	if w := call(h, "PATCH", "/internal/events/r1", `{"outcome":"pending"}`); w.Code != 422 {
		t.Fatalf("pending outcome %d", w.Code)
	}
}

func TestList(t *testing.T) {
	s := &memStore{}
	h := mux(s)
	call(h, "PUT", "/internal/events/r1", string(intentJSON(t, nil)))

	w := call(h, "GET", "/operator-audit-events?operator_id=op_1&limit=10", "")
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
	for _, k := range []string{"status", "response_bytes", "details", "created_id", "path"} {
		if v, ok := e[k]; !ok || v != nil {
			t.Errorf("%s = %v, want null", k, v)
		}
	}
	if e["service"] != "identity" || e["root_id"] != "01jbx9q3t5vk8m2n4p6r7s8t9w" || len(e["nodes"].([]any)) != 2 {
		t.Fatalf("%v", e)
	}
	if a := e["acting_as"].(map[string]any); a["member_id"] != "01jbyb0d1e2f3g4h5j6k7m8n9p" || a["principal"] != "person" {
		t.Fatalf("acting_as %v", a)
	}
	if _, ok := e["decision"]; ok {
		t.Error("decision is not a field")
	}
	if actor := e["actor"].(map[string]any); actor["email"] != nil || actor["client_id"] != "console" {
		t.Fatalf("actor %v", actor)
	}

	if w := call(h, "GET", "/operator-audit-events?organization_id=x", ""); w.Code != 422 {
		t.Fatalf("unknown filter %d", w.Code)
	}
	s.listErr = errors.New("down")
	if w := call(h, "GET", "/operator-audit-events", ""); w.Code != 500 || code(t, w) != "INTERNAL_ERROR" {
		t.Fatalf("store error %d", w.Code)
	}
}

func TestStoreErrorMapping(t *testing.T) {
	intentBody := string(intentJSON(t, nil))
	outcomeBody := `{"outcome":"responded","status":200,"response_bytes":0}`

	cases := []struct {
		name     string
		err      error
		wantHTTP int
		wantCode string
	}{
		{"deadline exceeded", context.DeadlineExceeded, 503, "SERVICE_UNAVAILABLE"},
		{"canceled", context.Canceled, 503, "SERVICE_UNAVAILABLE"},
		{"bad conn", driver.ErrBadConn, 503, "SERVICE_UNAVAILABLE"},
		{"pg connect error", &pgconn.ConnectError{}, 503, "SERVICE_UNAVAILABLE"},
		{"pg sqlstate 08", &pgconn.PgError{Code: "08006"}, 503, "SERVICE_UNAVAILABLE"},
		{"pg sqlstate 53", &pgconn.PgError{Code: "53300"}, 503, "SERVICE_UNAVAILABLE"},
		{"pg sqlstate 57", &pgconn.PgError{Code: "57P03"}, 503, "SERVICE_UNAVAILABLE"},
		{"unexpected error", errors.New("unexpected"), 500, "INTERNAL_ERROR"},
		{"pg data error", &pgconn.PgError{Code: "22P02"}, 500, "INTERNAL_ERROR"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &memStore{intentErr: c.err, outcomeErr: c.err, listErr: c.err, exportErr: c.err}
			h := mux(s)

			for _, req := range []struct {
				method, target, body string
			}{
				{"PUT", "/internal/events/r1", intentBody},
				{"PATCH", "/internal/events/r1", outcomeBody},
				{"GET", "/internal/events", ""},
				{"GET", "/operator-audit-events", ""},
			} {
				w := call(h, req.method, req.target, req.body)
				if w.Code != c.wantHTTP || code(t, w) != c.wantCode {
					t.Errorf("%s %s: status=%d code=%s, want %d %s", req.method, req.target, w.Code, code(t, w), c.wantHTTP, c.wantCode)
				}
			}
		})
	}
}

func TestExport(t *testing.T) {
	seq := int64(7)
	status := 200
	path := "/nodes"
	s := &memStore{
		exportEvents: []*Event{{
			ID:           newID(now),
			FinalizedSeq: &seq,
			Intent: Intent{
				RequestID:  "r-exp-1",
				OccurredAt: now,
				Actor:      Actor{Issuer: "https://idp", OperatorID: "op_1"},
				Method:     "GET",
				Path:       &path,
				IP:         "10.0.0.1",
			},
			Outcome: OutcomeResponded,
			Status:  &status,
		}},
		exportCursor: 7,
		exportMore:   true,
	}
	h := mux(s)

	w := call(h, "GET", "/internal/events?after=6&limit=5000", "")
	if w.Code != 200 {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	if s.exportAfter != 6 || s.exportLimit != maxExportLimit {
		t.Fatalf("parsed export: after=%d limit=%d", s.exportAfter, s.exportLimit)
	}
	var resp struct {
		AuditEvents []map[string]any `json:"audit_events"`
		Cursor      string           `json:"cursor"`
		HasMore     bool             `json:"has_more"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Cursor != "7" || !resp.HasMore || len(resp.AuditEvents) != 1 || resp.AuditEvents[0]["seq"] != "7" {
		t.Fatalf("export response: %s", w.Body)
	}

	// Cursor ahead -> 409 CURSOR_AHEAD.
	s.exportErr = ErrCursorAhead
	if w := call(h, "GET", "/internal/events?after=99", ""); w.Code != 409 || code(t, w) != "CURSOR_AHEAD" {
		t.Fatalf("cursor ahead: %d %s", w.Code, w.Body)
	}
	s.exportErr = nil

	for name, q := range map[string]string{
		"zero after":    "?after=0",
		"leading zero":  "?after=01",
		"negative":      "?after=-1",
		"non-int after": "?after=abc",
		"zero limit":    "?limit=0",
		"unknown":       "?foo=bar",
		"repeated":      "?limit=10&limit=20",
	} {
		t.Run(name, func(t *testing.T) {
			if w := call(h, "GET", "/internal/events"+q, ""); w.Code != 422 {
				t.Fatalf("want 422, got %d %s", w.Code, w.Body)
			}
		})
	}
}
