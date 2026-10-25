package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/identity"
	"github.com/plat5dev/operator/internal/metrics"
	"github.com/plat5dev/operator/internal/routes"
)

// Ids as Plat5 has them: lowercase ULIDs.
const (
	orgID          = "01jbx9q3t5vk8m2n4p6r7s8t9w"
	projectID      = "01jbx9r2c4dk6m8n0p2q4r6s8t"
	costCenterID   = "01jbx9s5e7fg9h1j3k5m7n9p1q"
	downNodeID     = "01jbx9t0w2x4y6z8a0b2c4d6e8"
	missingNodeID  = "01jbx9v3f5g7h9j1k3m5n7p9q1"
	forgedNodeID   = "01jbx9w6r8s0t2v4w6x8y0z2a4"
	someNodeID     = "01jbxa5m7n9p1q3r5s7t9v1w3x"
	someMemberID   = "01jbyb7g8h9j0k1m2n3p4q5r6s"
	danaID         = "01jbyb0d1e2f3g4h5j6k7m8n9p"
	ciID           = "01jbyb1q2r3s4t5v6w7x8y9z0a"
	goneMemberID   = "01jbyb2a3b4c5d6e7f8g9h0j1k"
	downMemberID   = "01jbyb3m4n5p6q7r8s9t0v1w2x"
	nobodyID       = "01jbyb4y5z6a7b8c9d0e1f2g3h"
	forgedMemberID = "01jbyb5j6k7m8n9p0q1r2s3t4v"
	userID         = "01jbzc0r1s2t3v4w5x6y7z8a9b"
	newNodeID      = "01jbx9x9b1c3d5e7f9g1h3j5k7"
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
		return &auth.Operator{ID: "op_1", Email: "a@x.test", Issuer: "https://idp.test", ClientID: "operator-console"}, nil
	case "Bearer old":
		return nil, auth.ErrExpired
	}
	return nil, auth.ErrInvalid
}

// fakeIdentity is a tree: orgID, projectID under it, costCenterID under that. downNodeID
// is identity failing. Members of orgID: danaID (person), ciID (service account),
// goneMemberID (removed).
type fakeIdentity struct{}

var tree = map[string]identity.Node{
	orgID:        {Kind: "organization", ID: orgID},
	projectID:    {Kind: "project", ID: projectID},
	costCenterID: {Kind: "cost_center", ID: costCenterID},
}

var parents = map[string]string{projectID: orgID, costCenterID: projectID}

func (fakeIdentity) Chain(_ context.Context, nodeID, requestID string) ([]identity.Node, error) {
	if !generatedID.MatchString(requestID) {
		return nil, errors.New("no request id")
	}
	if nodeID == downNodeID {
		return nil, identity.ErrUnavailable
	}
	n, ok := tree[nodeID]
	if !ok {
		return nil, identity.ErrNotFound
	}
	chain := []identity.Node{n}
	for p := parents[nodeID]; p != ""; p = parents[p] {
		chain = append([]identity.Node{tree[p]}, chain...)
	}
	return chain, nil
}

func (fakeIdentity) Member(_ context.Context, rootID, memberID, _ string) (string, error) {
	if rootID != orgID {
		return "", identity.ErrNotActive
	}
	switch memberID {
	case danaID:
		return "person", nil
	case ciID:
		return "service_account", nil
	case downMemberID:
		return "", identity.ErrUnavailable
	}
	return "", identity.ErrNotActive
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
	return setupWith(t, upstreamURL, a, timeout, nil, nil)
}

func setupWith(t *testing.T, upstreamURL string, a Authenticator, timeout time.Duration, aw AuditWriter, reg *metrics.Registry) (http.Handler, *bytes.Buffer) {
	t.Helper()
	tab, err := routes.Parse([]byte(`
services:
  identity:
    url: ` + upstreamURL + `
    routes:
      - path: /nodes
        methods: [GET]
      - path: /nodes/{node_id}
        node: node_id
        methods: [GET, PATCH]
      - path: /nodes/{root_id}/members
        node: root_id
        methods: [GET, POST]
      - path: /nodes/{node_id}/children/{kind}
        node: node_id
        as_member: true
        methods: [GET, POST]
      - path: /users/{user_id}
        methods: [GET]
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
		Identity:        fakeIdentity{},
		AllowedOrigins:  []string{"https://console.test"},
		UpstreamTimeout: timeout,
		Audit:           aw,
		Metrics:         reg,
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
		{"missing token", "GET", "/nodes", nil, 401, "UNAUTHORIZED", "missing"},
		{"bad token", "GET", "/nodes", map[string]string{"Authorization": "Bearer bad"}, 401, "UNAUTHORIZED", "invalid"},
		{"expired", "GET", "/nodes", map[string]string{"Authorization": "Bearer old"}, 401, "UNAUTHORIZED", "expired"},
		{"401 before 404", "GET", "/nope", nil, 401, "UNAUTHORIZED", "missing"},
		{"401 before 400", "GET", "/a/../nodes", nil, 401, "UNAUTHORIZED", "missing"},
		{"401 before node lookup", "GET", "/nodes/" + missingNodeID, nil, 401, "UNAUTHORIZED", "missing"},
		{"dot segment", "GET", "/nodes/x/../" + orgID + "/members", good, 400, "INVALID_REQUEST", ""},
		{"encoded slash", "GET", "/nodes/a%2Fb/members", good, 400, "INVALID_REQUEST", ""},
		{"double slash", "GET", "//nodes", good, 400, "INVALID_REQUEST", ""},
		{"trailing slash", "GET", "/nodes/", good, 400, "INVALID_REQUEST", ""},
		{"no route", "GET", "/users/" + userID + "/memberships", good, 404, "NOT_FOUND", ""},
		{"root", "GET", "/", good, 404, "NOT_FOUND", ""},
		{"method not listed", "DELETE", "/nodes", good, 404, "NOT_FOUND", ""},
		{"options without preflight", "OPTIONS", "/nodes", good, 404, "NOT_FOUND", ""},
		{"as on a route without as_member", "GET", "/as/" + danaID + "/nodes/" + orgID, good, 404, "NOT_FOUND", ""},
		{"as on no route", "GET", "/as/" + danaID + "/nodes", good, 404, "NOT_FOUND", ""},
		{"node not found", "GET", "/nodes/" + missingNodeID, good, 404, "NOT_FOUND", ""},
		{"member removed", "POST", "/as/" + goneMemberID + "/nodes/" + orgID + "/children/project", good, 400, "INVALID_REQUEST", ""},
		{"member of no tree", "POST", "/as/" + nobodyID + "/nodes/" + projectID + "/children/env", good, 400, "INVALID_REQUEST", ""},
		{"identity down for the node", "GET", "/nodes/" + downNodeID, good, 503, "SERVICE_UNAVAILABLE", ""},
		{"identity down for the member", "GET", "/as/" + downMemberID + "/nodes/" + orgID + "/children/project", good, 503, "SERVICE_UNAVAILABLE", ""},
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
			if c.name == "node not found" &&
				(e.Error.Details["resource"] != "node" || e.Error.Details["id"] != missingNodeID) {
				t.Fatalf("details = %v", e.Error.Details)
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
	w := do(h, "GET", "/nodes", map[string]string{"Authorization": "Bearer good"})
	if w.Code != 503 || decode(t, w).Error.Code != "SERVICE_UNAVAILABLE" {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestForward(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "upstream-id")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set(audit.CreatedHeader, "x_1")
		w.Header().Set("X-Plat5-Audit-Nodes", orgID)
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	h, logs := setup(t, up.srv.URL, fakeAuth{}, 0)
	w := do(h, "PATCH", "/nodes/%30"+projectID[1:]+"?limit=5&x=%2F", map[string]string{
		"Authorization":       "Bearer good",
		"Cookie":              "session=abc",
		"X-API-Key":           "key",
		"X-Request-ID":        "client-id.1",
		"X-Plat5-Member-Id":   forgedMemberID,
		"X-Plat5-Principal":   "person",
		"X-Plat5-Operator-Id": "op_forged",
		"X-Plat5-Project-Id":  forgedNodeID,
		"X-Plat5-Anything":    "x",
		"traceparent":         "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	})

	if w.Code != http.StatusTeapot || w.Body.String() != `{"ok":true}` {
		t.Fatalf("status %d body %q", w.Code, w.Body)
	}
	id := w.Header().Get("X-Request-ID")
	if got := w.Header().Values("X-Request-ID"); len(got) != 1 || !generatedID.MatchString(id) {
		t.Fatalf("response X-Request-ID = %v", got)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("upstream CORS header leaked")
	}
	if w.Header().Get("X-Upstream") != "yes" {
		t.Fatal("upstream header dropped")
	}
	for k := range w.Header() {
		if strings.HasPrefix(k, "X-Plat5-Audit-") {
			t.Fatalf("%s reached the client", k)
		}
	}
	for k, v := range securityHeaders {
		if got := w.Header().Values(k); len(got) != 1 || got[0] != v {
			t.Fatalf("%s = %v", k, got)
		}
	}

	s := up.seen()
	if s.Path != "/nodes/%30"+projectID[1:] {
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
	if s.Header.Get("X-Request-ID") != id {
		t.Fatalf("upstream X-Request-ID = %q", s.Header.Get("X-Request-ID"))
	}
	if s.Header.Get("traceparent") == "" {
		t.Fatal("traceparent dropped")
	}
	// The chain and the operator, as this gateway set them. Nothing a client sent.
	wantPlat5 := map[string]string{
		"X-Plat5-Organization-Id": orgID,
		"X-Plat5-Project-Id":      projectID,
		"X-Plat5-Operator-Id":     "op_1",
	}
	checkPlat5(t, s.Header, wantPlat5)

	line := lastLog(t, logs)
	want := map[string]any{
		"msg": "request", "request_id": id, "method": "PATCH", "status": float64(418),
		"operator_id": "op_1", "operator_email": "a@x.test", "service": "identity", "route": "/nodes/{node_id}",
		"root_id": orgID, "node_id": projectID,
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("log %s = %v, want %v", k, line[k], v)
		}
	}
	if line["member_id"] != nil {
		t.Errorf("log member_id = %v", line["member_id"])
	}
	if p, _ := line["params"].(map[string]any); p["node_id"] != projectID {
		t.Errorf("log params = %v", line["params"])
	}
}

// checkPlat5 fails unless the X-Plat5-* headers are exactly want, one value each.
func checkPlat5(t *testing.T, h http.Header, want map[string]string) {
	t.Helper()
	for k, v := range h {
		if !strings.HasPrefix(k, "X-Plat5-") {
			continue
		}
		if len(v) != 1 || v[0] != want[k] {
			t.Errorf("upstream %s = %v, want %q", k, v, want[k])
		}
	}
	for k, v := range want {
		if h.Get(k) != v {
			t.Errorf("upstream %s = %q, want %q", k, h.Get(k), v)
		}
	}
}

func TestActingAsMember(t *testing.T) {
	cases := []struct {
		name, path, upstreamPath string
		want                     map[string]string
		member                   string
	}{
		{"person", "/as/" + danaID + "/nodes/" + orgID + "/children/project", "/nodes/" + orgID + "/children/project",
			map[string]string{"X-Plat5-Organization-Id": orgID, "X-Plat5-Member-Id": danaID, "X-Plat5-Principal": "person"}, danaID},
		{"service account, escaped", "/as/%30" + ciID[1:] + "/nodes/%30" + costCenterID[1:] + "/children/x%20y", "/nodes/%30" + costCenterID[1:] + "/children/x%20y",
			map[string]string{"X-Plat5-Organization-Id": orgID, "X-Plat5-Project-Id": projectID, "X-Plat5-Cost-Center-Id": costCenterID,
				"X-Plat5-Member-Id": ciID, "X-Plat5-Principal": "service_account"}, ciID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			up := newUpstream(t, nil)
			h, logs := setup(t, up.srv.URL, fakeAuth{}, 0)
			w := do(h, "POST", c.path+"?q=1", map[string]string{"Authorization": "Bearer good", "X-Plat5-Member-Id": forgedMemberID})
			if w.Code != http.StatusTeapot {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			s := up.seen()
			if s.Path != c.upstreamPath || s.Query != "q=1" {
				t.Fatalf("upstream %s?%s", s.Path, s.Query)
			}
			c.want["X-Plat5-Operator-Id"] = "op_1"
			checkPlat5(t, s.Header, c.want)
			line := lastLog(t, logs)
			if line["member_id"] != c.member || line["route"] != "/as/{member}/nodes/{node_id}/children/{kind}" {
				t.Fatalf("log = %v", line)
			}
		})
	}

	// Without /as/, the same route sends no member and no principal.
	up := newUpstream(t, nil)
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 0)
	do(h, "GET", "/nodes/"+orgID+"/children/project", map[string]string{"Authorization": "Bearer good"})
	checkPlat5(t, up.seen().Header, map[string]string{"X-Plat5-Organization-Id": orgID, "X-Plat5-Operator-Id": "op_1"})
}

func TestNoNodeNoChain(t *testing.T) {
	up := newUpstream(t, nil)
	h, logs := setup(t, up.srv.URL, fakeAuth{}, 0)
	do(h, "GET", "/users/"+userID, map[string]string{"Authorization": "Bearer good"})
	checkPlat5(t, up.seen().Header, map[string]string{"X-Plat5-Operator-Id": "op_1"})
	if line := lastLog(t, logs); line["root_id"] != nil || line["node_id"] != nil {
		t.Fatalf("log = %v", line)
	}
}

func TestStripAs(t *testing.T) {
	for in, want := range map[string]string{
		"/as/" + someMemberID + "/nodes/" + someNodeID: "/nodes/" + someNodeID,
		"/as/m%2F1/nodes":                "/nodes",
		"/as/" + someMemberID + "/a/b/c": "/a/b/c",
		"/nodes":                         "/nodes",
		"/as/" + someMemberID:            "/as/" + someMemberID,
	} {
		if got := stripAs(in); got != want {
			t.Errorf("stripAs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKindHeader(t *testing.T) {
	for kind, want := range map[string]string{
		"organization": "X-Plat5-Organization-Id",
		"project":      "X-Plat5-Project-Id",
		"cost_center":  "X-Plat5-Cost-Center-Id",
		"v2_env":       "X-Plat5-V2-Env-Id",
	} {
		if got := kindHeader(kind); got != want {
			t.Errorf("kindHeader(%q) = %q, want %q", kind, got, want)
		}
	}
}

var generatedID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestRequestIDAlwaysGenerated(t *testing.T) {
	up := newUpstream(t, nil)
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 0)
	seen := map[string]bool{}
	for _, client := range []string{"", "client-id.1", "bad id\n"} {
		w := do(h, "GET", "/nodes", map[string]string{"Authorization": "Bearer good", "X-Request-ID": client})
		id := w.Header().Get("X-Request-ID")
		if !generatedID.MatchString(id) || seen[id] || up.seen().Header.Get("X-Request-ID") != id {
			t.Fatalf("client %q: id = %q, upstream %q", client, id, up.seen().Header.Get("X-Request-ID"))
		}
		seen[id] = true
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
	w := do(h, "GET", "/nodes", map[string]string{"Authorization": "Bearer good"})
	if w.Code != 502 || decode(t, w).Error.Code != "SERVICE_UNAVAILABLE" {
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
	w := do(h, "GET", "/nodes", map[string]string{"Authorization": "Bearer good"})
	if w.Code != 502 {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestCORS(t *testing.T) {
	up := newUpstream(t, nil)
	h, _ := setup(t, up.srv.URL, fakeAuth{}, 0)

	w := do(h, "OPTIONS", "/nodes", map[string]string{
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

	w = do(h, "OPTIONS", "/nodes", map[string]string{
		"Origin":                        "https://evil.test",
		"Access-Control-Request-Method": "GET",
	})
	if w.Code != 401 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed preflight: %d %v", w.Code, w.Header())
	}

	w = do(h, "GET", "/nodes", map[string]string{"Origin": "https://console.test"})
	if w.Code != 401 || w.Header().Get("Access-Control-Allow-Origin") != "https://console.test" ||
		w.Header().Get("Access-Control-Expose-Headers") != "X-Request-ID" {
		t.Fatalf("401 must be readable by the allowed origin: %v", w.Header())
	}

	w = do(h, "GET", "/nodes", map[string]string{"Origin": "https://console.test", "Authorization": "Bearer good"})
	if got := w.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != "https://console.test" {
		t.Fatalf("forwarded ACAO = %v", got)
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q", w.Header().Get("Vary"))
	}
}

func TestUnauthenticatedIsLogged(t *testing.T) {
	h, logs := setup(t, "http://127.0.0.1:1", fakeAuth{}, 0)
	do(h, "GET", "/nodes", nil)
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
