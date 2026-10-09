package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/metrics"
)

// recordingAudit keeps what the gateway writes. err makes every intent fail.
type recordingAudit struct {
	mu       sync.Mutex
	err      error
	intents  []*audit.Intent
	outcomes map[string]audit.Outcome
}

func (a *recordingAudit) WriteIntent(_ context.Context, in *audit.Intent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intents = append(a.intents, in)
	return a.err
}

func (a *recordingAudit) SendOutcome(requestID string, out audit.Outcome) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.outcomes == nil {
		a.outcomes = map[string]audit.Outcome{}
	}
	if _, dup := a.outcomes[requestID]; dup {
		panic("two outcomes for " + requestID)
	}
	a.outcomes[requestID] = out
}

func (a *recordingAudit) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.intents)
}

// only is the one intent and its outcome. The test fails unless there is exactly one of each.
func (a *recordingAudit) only(t *testing.T, w *httptest.ResponseRecorder) (*audit.Intent, audit.Outcome) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.intents) != 1 || len(a.outcomes) != 1 {
		t.Fatalf("%d intents, %d outcomes", len(a.intents), len(a.outcomes))
	}
	in := a.intents[0]
	if in.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatalf("intent for %q, response %q", in.RequestID, w.Header().Get("X-Request-ID"))
	}
	out, ok := a.outcomes[in.RequestID]
	if !ok {
		t.Fatal("outcome is for another request")
	}
	return in, out
}

func TestAuditForwarded(t *testing.T) {
	rec := &recordingAudit{}
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if rec.count() != 1 {
			t.Error("the service was called before the intent was written")
		}
		w.Header().Set(audit.DetailsHeader, `{"role":{"from":"developer","to":"admin"}}`)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m_1"}`)
	})
	h, _ := setupWith(t, up.srv.URL, fakeAuth{}, 0, rec, nil)
	w := do(h, "POST", "/organizations/org_1/members?q=secret", map[string]string{
		"Authorization":     "Bearer good",
		"User-Agent":        "console/1.0",
		audit.DetailsHeader: `{"forged":true}`,
	})
	if w.Code != 200 || w.Header().Get(audit.DetailsHeader) != "" {
		t.Fatalf("status %d, details header %q", w.Code, w.Header().Get(audit.DetailsHeader))
	}
	if up.seen().Header.Get(audit.DetailsHeader) != "" {
		t.Fatal("a client's details header reached the upstream")
	}

	in, out := rec.only(t, w)
	b, _ := json.Marshal(in)
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	occurred, err := time.Parse(time.RFC3339Nano, got["occurred_at"].(string))
	if err != nil || time.Since(occurred) > time.Minute || !strings.HasSuffix(got["occurred_at"].(string), "Z") {
		t.Fatalf("occurred_at %v", got["occurred_at"])
	}
	delete(got, "occurred_at")
	want := map[string]any{
		"actor":      map[string]any{"issuer": "https://idp.test", "operator_id": "op_1", "email": "a@x.test", "client_id": "operator-console"},
		"upstream":   "identity",
		"method":     "POST",
		"route":      "/organizations/{organization_id}/members",
		"params":     map[string]any{"organization_id": "org_1"},
		"path":       nil,
		"ip":         "192.0.2.1",
		"user_agent": "console/1.0",
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("intent\n got %s\nwant %s", gotJSON, wantJSON)
	}

	if out.Outcome != audit.Responded || *out.Status != 200 || out.ResponseBytes != int64(len(`{"id":"m_1"}`)) ||
		string(out.Details) != `{"role":{"from":"developer","to":"admin"}}` {
		t.Fatalf("outcome %+v details %s", out, out.Details)
	}
}

func TestAuditRejected(t *testing.T) {
	cases := []struct {
		name, method, path string
		status             int
		wantPath           string
	}{
		{"unacceptable path", "GET", "/organizations/x/../org_1/members", 400, "/organizations/x/../org_1/members"},
		{"no route", "GET", "/users/u_1/memberships", 404, "/users/u_1/memberships"},
		{"method not listed", "DELETE", "/organizations", 404, "/organizations"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recordingAudit{}
			up := newUpstream(t, nil)
			h, _ := setupWith(t, up.srv.URL, fakeAuth{}, 0, rec, nil)
			w := do(h, c.method, c.path, map[string]string{"Authorization": "Bearer good"})
			if w.Code != c.status {
				t.Fatalf("status %d", w.Code)
			}
			in, out := rec.only(t, w)
			if in.Route != nil || in.Upstream != nil || in.Path == nil || *in.Path != c.wantPath || len(in.Params) != 0 {
				t.Fatalf("intent route %v upstream %v path %v params %v", in.Route, in.Upstream, in.Path, in.Params)
			}
			if out.Outcome != audit.Rejected || *out.Status != c.status || out.Details != nil {
				t.Fatalf("outcome %+v", out)
			}
			if up.seen() != nil {
				t.Fatal("a rejected request reached the upstream")
			}
		})
	}
}

func TestAuditSkipsUnauthenticated(t *testing.T) {
	rec := &recordingAudit{}
	reg := metrics.NewRegistry()
	h, _ := setupWith(t, "http://127.0.0.1:1", fakeAuth{}, 0, rec, reg)
	do(h, "GET", "/organizations", nil)
	do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer bad"})
	do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer old"})
	do(h, "OPTIONS", "/organizations", map[string]string{"Origin": "https://console.test", "Access-Control-Request-Method": "GET"})
	if rec.count() != 0 || len(rec.outcomes) != 0 {
		t.Fatalf("%d intents, %d outcomes", rec.count(), len(rec.outcomes))
	}
	m := httptest.NewRecorder()
	reg.ServeHTTP(m, httptest.NewRequest("GET", "/metrics", nil))
	for _, line := range []string{
		`operator_unauthenticated_total{reason="missing"} 1`,
		`operator_unauthenticated_total{reason="invalid"} 1`,
		`operator_unauthenticated_total{reason="expired"} 1`,
	} {
		if !strings.Contains(m.Body.String(), line) {
			t.Errorf("metrics missing %s", line)
		}
	}

	h, _ = setupWith(t, "http://127.0.0.1:1", fakeAuth{err: auth.ErrNoKeys}, 0, rec, nil)
	if w := do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer good"}); w.Code != 503 || rec.count() != 0 {
		t.Fatalf("no keys: status %d, %d intents", w.Code, rec.count())
	}
}

func TestAuditIntentNotWritten(t *testing.T) {
	rec := &recordingAudit{err: audit.ErrIntentNotWritten}
	up := newUpstream(t, nil)
	h, _ := setupWith(t, up.srv.URL, fakeAuth{}, 0, rec, nil)
	for _, path := range []string{"/organizations", "/nope", "//organizations"} {
		w := do(h, "GET", path, map[string]string{"Authorization": "Bearer good"})
		if w.Code != 503 || decode(t, w).Error.Code != "SERVICE_UNAVAILABLE" {
			t.Fatalf("%s: status %d", path, w.Code)
		}
		out := rec.outcomes[w.Header().Get("X-Request-ID")]
		if out.Outcome != audit.Rejected || *out.Status != 503 {
			t.Fatalf("%s: outcome %+v", path, out)
		}
	}
	if up.seen() != nil {
		t.Fatal("the service was called without an intent")
	}
}

func TestAuditNoResponse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	rec := &recordingAudit{}
	h, _ := setupWith(t, "http://"+addr, fakeAuth{}, 0, rec, nil)
	w := do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer good"})
	_, out := rec.only(t, w)
	if w.Code != 503 || out.Outcome != audit.NoResponse || *out.Status != 503 {
		t.Fatalf("status %d outcome %+v", w.Code, out)
	}
}

func TestAuditClientGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	})
	rec := &recordingAudit{}
	h, _ := setupWith(t, up.srv.URL, fakeAuth{}, 0, rec, nil)
	r := httptest.NewRequest("GET", "/organizations", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer good")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	_, out := rec.only(t, w)
	if out.Outcome != audit.NoResponse || out.Status != nil {
		t.Fatalf("outcome %+v", out)
	}
}

func TestAuditDetailsUnusable(t *testing.T) {
	rec := &recordingAudit{}
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(audit.DetailsHeader, `[1,2]`)
		w.WriteHeader(http.StatusNoContent)
	})
	h, logs := setupWith(t, up.srv.URL, fakeAuth{}, 0, rec, nil)
	w := do(h, "PATCH", "/organizations/org_1", map[string]string{"Authorization": "Bearer good"})
	if w.Header().Get(audit.DetailsHeader) != "" {
		t.Fatal("details header reached the client")
	}
	_, out := rec.only(t, w)
	if out.Outcome != audit.Responded || out.Details != nil {
		t.Fatalf("outcome %+v", out)
	}
	if !strings.Contains(logs.String(), "ignoring audit details") {
		t.Fatal("unusable details not logged")
	}
}

func TestAuditParamsAreStorable(t *testing.T) {
	rec := &recordingAudit{}
	up := newUpstream(t, nil)
	h, _ := setupWith(t, up.srv.URL, fakeAuth{}, 0, rec, nil)
	w := do(h, "GET", "/organizations/a%00b%FF/members", map[string]string{"Authorization": "Bearer good"})
	in, _ := rec.only(t, w)
	if got := in.Params["organization_id"]; got != "a�b�" {
		t.Fatalf("param %q", got)
	}
}

func TestDetailsStrippedWithAuditOff(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(audit.DetailsHeader, `{"a":1}`)
		w.WriteHeader(http.StatusOK)
	})
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 0)
	w := do(h, "PATCH", "/organizations/org_1", map[string]string{"Authorization": "Bearer good", audit.DetailsHeader: `{"b":2}`})
	if w.Code != 200 || w.Header().Get(audit.DetailsHeader) != "" || up.seen().Header.Get(audit.DetailsHeader) != "" {
		t.Fatalf("status %d, response %q, upstream %q", w.Code, w.Header().Get(audit.DetailsHeader), up.seen().Header.Get(audit.DetailsHeader))
	}
}
