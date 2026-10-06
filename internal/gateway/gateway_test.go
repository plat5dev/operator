package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/routes"
)

type fakeAuth struct{ err error }

func (f fakeAuth) Verify(_ context.Context, header string) (*auth.Operator, error) {
	if f.err != nil {
		return nil, f.err
	}
	switch header {
	case "":
		return nil, auth.ErrMissing
	case "Bearer good":
		return &auth.Operator{ID: "op_1", Email: "a@x.test"}, nil
	case "Bearer old":
		return nil, auth.ErrExpired
	}
	return nil, auth.ErrInvalid
}

type seen struct {
	Path   string
	Query  string
	Header http.Header
	Body   string
}

type upstream struct {
	mu   sync.Mutex
	last *seen
	srv  *httptest.Server
}

func newUpstream(t *testing.T, h http.HandlerFunc) *upstream {
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.last = &seen{Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: string(b)}
		u.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Header().Set("X-Request-ID", "upstream-id")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) seen() *seen {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last
}

func setup(t *testing.T, upstreamURL string, a Authenticator, timeout time.Duration) (http.Handler, *bytes.Buffer) {
	t.Helper()
	tab, err := routes.Parse([]byte(`
upstreams:
  identity:
    url: ` + upstreamURL + `
    routes:
      - path: /organizations
        methods: [GET]
      - path: /organizations/{organization_id}/members
        methods: [GET, POST]
`))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	g := New(Config{
		Routes:          tab,
		Auth:            a,
		AllowedOrigins:  []string{"https://console.test"},
		UpstreamTimeout: timeout,
		Log:             slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	return g, &logs
}

func do(h http.Handler, method, target string, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(`{"a":1}`))
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type envelope struct {
	Error struct {
		Type      string         `json:"type"`
		Code      string         `json:"code"`
		Message   string         `json:"message"`
		RequestID string         `json:"request_id"`
		Details   map[string]any `json:"details"`
	} `json:"error"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if e.Error.RequestID == "" || e.Error.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatalf("request_id %q, header %q", e.Error.RequestID, w.Header().Get("X-Request-ID"))
	}
	return e
}

func TestRejections(t *testing.T) {
	up := newUpstream(t, nil)
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 0)
	good := map[string]string{"Authorization": "Bearer good"}

	cases := []struct {
		name, method, path string
		header             map[string]string
		status             int
		code, reason       string
	}{
		{"missing token", "GET", "/organizations", nil, 401, "UNAUTHORIZED", "missing"},
		{"bad token", "GET", "/organizations", map[string]string{"Authorization": "Bearer bad"}, 401, "UNAUTHORIZED", "invalid"},
		{"expired", "GET", "/organizations", map[string]string{"Authorization": "Bearer old"}, 401, "UNAUTHORIZED", "expired"},
		{"401 before 404", "GET", "/nope", nil, 401, "UNAUTHORIZED", "missing"},
		{"401 before 400", "GET", "/a/../organizations", nil, 401, "UNAUTHORIZED", "missing"},
		{"dot segment", "GET", "/organizations/x/../org_1/members", good, 400, "INVALID_REQUEST", ""},
		{"encoded slash", "GET", "/organizations/a%2Fb/members", good, 400, "INVALID_REQUEST", ""},
		{"double slash", "GET", "//organizations", good, 400, "INVALID_REQUEST", ""},
		{"trailing slash", "GET", "/organizations/", good, 400, "INVALID_REQUEST", ""},
		{"no route", "GET", "/users/u_1/memberships", good, 404, "NOT_FOUND", ""},
		{"root", "GET", "/", good, 404, "NOT_FOUND", ""},
		{"method not listed", "DELETE", "/organizations", good, 404, "NOT_FOUND", ""},
		{"options without preflight", "OPTIONS", "/organizations", good, 404, "NOT_FOUND", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := do(h, c.method, c.path, c.header)
			if w.Code != c.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, c.status, w.Body)
			}
			e := decode(t, w)
			if e.Error.Code != c.code {
				t.Fatalf("code = %s", e.Error.Code)
			}
			if c.status == 401 {
				if w.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Fatal("missing WWW-Authenticate")
				}
				if e.Error.Details["reason"] != c.reason {
					t.Fatalf("reason = %v", e.Error.Details["reason"])
				}
			}
		})
	}
	if up.seen() != nil {
		t.Fatal("a rejected request reached the upstream")
	}
}

func TestNoKeysIs503(t *testing.T) {
	h, _ := setup(t, "http://127.0.0.1:1", fakeAuth{err: auth.ErrNoKeys}, 0)
	w := do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer good"})
	if w.Code != 503 || decode(t, w).Error.Code != "SERVICE_UNAVAILABLE" {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestForward(t *testing.T) {
	up := newUpstream(t, nil)
	h, logs := setup(t, up.srv.URL, fakeAuth{}, 0)
	w := do(h, "POST", "/organizations/org%5F1/members?limit=5&x=%2F", map[string]string{
		"Authorization": "Bearer good",
		"Cookie":        "session=abc",
		"X-API-Key":     "key",
		"X-Request-ID":  "client-id.1",
		"traceparent":   "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	})

	if w.Code != http.StatusTeapot || w.Body.String() != `{"ok":true}` {
		t.Fatalf("status %d body %q", w.Code, w.Body)
	}
	if got := w.Header().Values("X-Request-ID"); len(got) != 1 || got[0] != "client-id.1" {
		t.Fatalf("response X-Request-ID = %v", got)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("upstream CORS header leaked")
	}
	if w.Header().Get("X-Upstream") != "yes" {
		t.Fatal("upstream header dropped")
	}

	s := up.seen()
	if s.Path != "/organizations/org%5F1/members" {
		t.Fatalf("upstream path = %q", s.Path)
	}
	if s.Query != "limit=5&x=%2F" {
		t.Fatalf("upstream query = %q", s.Query)
	}
	if s.Body != `{"a":1}` {
		t.Fatalf("upstream body = %q", s.Body)
	}
	for _, h := range []string{"Authorization", "Cookie", "X-API-Key"} {
		if s.Header.Get(h) != "" {
			t.Fatalf("%s reached the upstream", h)
		}
	}
	if s.Header.Get("X-Request-ID") != "client-id.1" {
		t.Fatalf("upstream X-Request-ID = %q", s.Header.Get("X-Request-ID"))
	}
	if s.Header.Get("traceparent") == "" {
		t.Fatal("traceparent dropped")
	}
	for k := range s.Header {
		if strings.Contains(strings.ToLower(k), "operator") {
			t.Fatalf("actor header %s reached the upstream", k)
		}
	}

	line := lastLog(t, logs)
	want := map[string]any{
		"msg": "request", "request_id": "client-id.1", "method": "POST", "status": float64(418),
		"operator_id": "op_1", "operator_email": "a@x.test", "route": "/organizations/{organization_id}/members",
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("log %s = %v, want %v", k, line[k], v)
		}
	}
	if p, _ := line["params"].(map[string]any); p["organization_id"] != "org_1" {
		t.Errorf("log params = %v", line["params"])
	}
}

func TestRequestIDReplacedWhenInvalid(t *testing.T) {
	up := newUpstream(t, nil)
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 0)
	w := do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer good", "X-Request-ID": "bad id\n"})
	id := w.Header().Get("X-Request-ID")
	if id == "" || id == "bad id\n" || up.seen().Header.Get("X-Request-ID") != id {
		t.Fatalf("id = %q, upstream %q", id, up.seen().Header.Get("X-Request-ID"))
	}
}

func TestUpstreamDown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	h, _ := setup(t, "http://"+addr, fakeAuth{}, 0)
	w := do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer good"})
	if w.Code != 503 || decode(t, w).Error.Code != "SERVICE_UNAVAILABLE" {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestUpstreamTimeout(t *testing.T) {
	release := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 50*time.Millisecond)
	w := do(h, "GET", "/organizations", map[string]string{"Authorization": "Bearer good"})
	if w.Code != 503 {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestCORS(t *testing.T) {
	up := newUpstream(t, nil)
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 0)

	w := do(h, "OPTIONS", "/organizations", map[string]string{
		"Origin":                        "https://console.test",
		"Access-Control-Request-Method": "GET",
	})
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://console.test" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials mode allowed")
	}
	if up.seen() != nil {
		t.Fatal("preflight forwarded")
	}

	w = do(h, "OPTIONS", "/organizations", map[string]string{
		"Origin":                        "https://evil.test",
		"Access-Control-Request-Method": "GET",
	})
	if w.Code != 401 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed preflight: %d %v", w.Code, w.Header())
	}

	w = do(h, "GET", "/organizations", map[string]string{"Origin": "https://console.test"})
	if w.Code != 401 || w.Header().Get("Access-Control-Allow-Origin") != "https://console.test" ||
		w.Header().Get("Access-Control-Expose-Headers") != "X-Request-ID" {
		t.Fatalf("401 must be readable by the allowed origin: %v", w.Header())
	}

	w = do(h, "GET", "/organizations", map[string]string{"Origin": "https://console.test", "Authorization": "Bearer good"})
	if got := w.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != "https://console.test" {
		t.Fatalf("forwarded ACAO = %v", got)
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q", w.Header().Get("Vary"))
	}
}

func TestUnauthenticatedIsLogged(t *testing.T) {
	h, logs := setup(t, "http://127.0.0.1:1", fakeAuth{}, 0)
	do(h, "GET", "/organizations", nil)
	line := lastLog(t, logs)
	if line["status"] != float64(401) || line["operator_id"] != nil || line["route"] != nil {
		t.Fatalf("log = %v", line)
	}
}

func lastLog(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	var last map[string]any
	sc := bufio.NewScanner(bytes.NewReader(logs.Bytes()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m["msg"] == "request" {
			last = m
		}
	}
	if last == nil {
		t.Fatal("no request log")
	}
	return last
}
