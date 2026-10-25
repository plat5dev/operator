package audit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plat5dev/operator/internal/metrics"
)

// fakeAudit answers each request with the next status in statuses; the last repeats.
// Status 0 hangs until the client gives up.
type fakeAudit struct {
	mu       sync.Mutex
	statuses []int
	hits     atomic.Int64
	reqs     []*http.Request
	bodies   []string
	srv      *httptest.Server
}

func newFake(t *testing.T, statuses ...int) *fakeAudit {
	f := &fakeAudit{statuses: statuses}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(f.hits.Add(1)) - 1
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, r)
		f.bodies = append(f.bodies, string(b))
		status := f.statuses[min(n, len(f.statuses)-1)]
		f.mu.Unlock()
		if status == 0 {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAudit) last() (*http.Request, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1], f.bodies[len(f.bodies)-1]
}

func writer(t *testing.T, url string) (*Writer, *metrics.Registry) {
	reg := metrics.NewRegistry()
	w := New(Config{URL: url + "/", Token: "tok", Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Metrics: reg})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = w.Close(ctx)
	})
	return w, reg
}

func intent() *Intent {
	route := "/nodes/{node_id}"
	service := "identity"
	root := "01jbx9q3t5vk8m2n4p6r7s8t9w"
	return &Intent{
		RequestID:  "req1",
		OccurredAt: Timestamp(time.Now()),
		Actor:      Actor{Issuer: "https://idp.test", OperatorID: "op_1"},
		RootID:     &root,
		Nodes:      []Node{{Kind: "organization", ID: "01jbx9q3t5vk8m2n4p6r7s8t9w"}},
		Service:    &service,
		Method:     "GET",
		Route:      &route,
		Params:     map[string]string{"node_id": "01jbx9q3t5vk8m2n4p6r7s8t9w"},
		IP:         "127.0.0.1",
	}
}

func TestIntentWire(t *testing.T) {
	f := newFake(t, 201)
	w, reg := writer(t, f.srv.URL)
	if err := w.WriteIntent(context.Background(), intent()); err != nil {
		t.Fatal(err)
	}
	r, body := f.last()
	if r.Method != "PUT" || r.URL.Path != "/internal/events/req1" {
		t.Fatalf("%s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("X-Request-ID") != "req1" {
		t.Fatalf("headers %v", r.Header)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["request_id"]; ok {
		t.Fatal("request_id is in the URL, not the body")
	}
	for _, k := range []string{"path", "user_agent"} {
		if v, ok := m[k]; !ok || v != nil {
			t.Fatalf("%s = %v, want null", k, v)
		}
	}
	actor := m["actor"].(map[string]any)
	if v, ok := actor["email"]; !ok || v != nil {
		t.Fatalf("email = %v", v)
	}
	if got := scrape(reg); !strings.Contains(got, `operator_audit_writes_total{write="intent",result="ok"} 1`) {
		t.Fatal(got)
	}
}

func TestIntentRetriesA5xx(t *testing.T) {
	f := newFake(t, 503, 200)
	w, _ := writer(t, f.srv.URL)
	if err := w.WriteIntent(context.Background(), intent()); err != nil {
		t.Fatal(err)
	}
	if f.hits.Load() != 2 {
		t.Fatalf("hits = %d", f.hits.Load())
	}
}

func TestIntentDoesNotRetryA4xx(t *testing.T) {
	f := newFake(t, 401)
	w, _ := writer(t, f.srv.URL)
	if err := w.WriteIntent(context.Background(), intent()); err == nil {
		t.Fatal("want error")
	}
	if f.hits.Load() != 1 {
		t.Fatalf("hits = %d", f.hits.Load())
	}
}

func TestIntentGivesUpWithinTheBudget(t *testing.T) {
	f := newFake(t, 0)
	w, _ := writer(t, f.srv.URL)
	start := time.Now()
	if err := w.WriteIntent(context.Background(), intent()); err == nil {
		t.Fatal("want error")
	}
	if took := time.Since(start); took > intentBudget+250*time.Millisecond {
		t.Fatalf("took %v", took)
	}
	if f.hits.Load() != 2 {
		t.Fatalf("two 500ms attempts fill the 1s budget; hits = %d", f.hits.Load())
	}
}

func TestIntentIgnoresClientCancel(t *testing.T) {
	f := newFake(t, 201)
	w, _ := writer(t, f.srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.WriteIntent(ctx, intent()); err != nil {
		t.Fatal(err)
	}
}

func TestOutcomeRetriesAndStops(t *testing.T) {
	cases := []struct {
		name     string
		statuses []int
		hits     int64
		result   string
	}{
		{"retries until taken", []int{503, 503, 204}, 3, "ok"},
		{"stops on 404", []int{404}, 1, "not_found"},
		{"stops on 422", []int{422}, 1, "failed"},
		{"gives up after backoff", []int{503}, 3, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t, c.statuses...)
			w, _ := writer(t, f.srv.URL)
			w.backoff = []time.Duration{time.Millisecond, time.Millisecond}
			w.writeOutcome(queued{requestID: "req1", outcome: OutcomeFor(&State{}, 404, 10)})
			if f.hits.Load() != c.hits {
				t.Fatalf("hits = %d", f.hits.Load())
			}
			if w.writes.Value("outcome", c.result) != 1 {
				t.Fatalf("result %s not counted", c.result)
			}
			r, body := f.last()
			if r.Method != "PATCH" || r.URL.Path != "/internal/events/req1" {
				t.Fatalf("%s %s", r.Method, r.URL.Path)
			}
			if body != `{"outcome":"rejected","status":404,"response_bytes":10,"details":null,"created_id":null}` {
				t.Fatalf("body %s", body)
			}
		})
	}
}

func TestCloseSendsWhatIsQueued(t *testing.T) {
	f := newFake(t, 204)
	w, _ := writer(t, f.srv.URL)
	for range 5 {
		w.SendOutcome("req1", OutcomeFor(&State{}, 503, 0))
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.hits.Load() != 5 {
		t.Fatalf("hits = %d", f.hits.Load())
	}
	w.SendOutcome("req2", OutcomeFor(&State{}, 503, 0))
	if w.writes.Value("outcome", "dropped") != 1 {
		t.Fatal("an outcome after close must be dropped")
	}
}

func TestCloseGivesUpAtTheDeadline(t *testing.T) {
	f := newFake(t, 503)
	w, _ := writer(t, f.srv.URL)
	w.SendOutcome("req1", OutcomeFor(&State{}, 503, 0))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := w.Close(ctx); err == nil {
		t.Fatal("want error: the outcome was never taken")
	}
	if time.Since(start) > time.Second {
		t.Fatal("close waited past its deadline")
	}
	if w.writes.Value("outcome", "dropped") != 1 {
		t.Fatal("the abandoned outcome must be counted")
	}
}

func TestOutcomeFor(t *testing.T) {
	details := json.RawMessage(`{"a":1}`)
	cases := []struct {
		name    string
		state   State
		written int
		want    string
	}{
		{"responded", State{Forwarded: true, UpstreamStatus: 200, Details: details}, 200,
			`{"outcome":"responded","status":200,"response_bytes":7,"details":{"a":1},"created_id":null}`},
		{"responded, client gone mid-body", State{Forwarded: true, UpstreamStatus: 200, ClientGone: true}, 200,
			`{"outcome":"responded","status":200,"response_bytes":7,"details":null,"created_id":null}`},
		{"no response", State{Forwarded: true}, 503,
			`{"outcome":"no_response","status":503,"response_bytes":7,"details":null,"created_id":null}`},
		{"no response, client gone", State{Forwarded: true, ClientGone: true}, 503,
			`{"outcome":"no_response","status":null,"response_bytes":7,"details":null,"created_id":null}`},
		{"rejected", State{}, 404,
			`{"outcome":"rejected","status":404,"response_bytes":7,"details":null,"created_id":null}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, _ := json.Marshal(OutcomeFor(&c.state, c.written, 7))
			if string(b) != c.want {
				t.Fatalf("%s", b)
			}
		})
	}
}

func TestParseDetails(t *testing.T) {
	ok := []string{` {"role":{"from":"developer","to":"admin"}} `, "{\"name\":\"\\u00e9\"}"}
	for _, v := range ok {
		if _, good := ParseDetails([]string{v}); !good {
			t.Errorf("%q rejected", v)
		}
	}
	bad := []string{`[1,2]`, `"text"`, `{"a":`, "{\"name\":\"é\"}", "{\"a\":\"\x01\"}", `{"a":"\u0000"}`,
		`{"a":"` + strings.Repeat("x", MaxDetailsBytes) + `"}`}
	for _, v := range bad {
		if _, good := ParseDetails([]string{v}); good {
			t.Errorf("%q accepted", v)
		}
	}
	if _, good := ParseDetails([]string{`{}`, `{}`}); good {
		t.Error("two headers accepted")
	}
}

func TestText(t *testing.T) {
	if got := Text("a\x00b\xffc"); got != "a�b�c" {
		t.Fatalf("%q", got)
	}
	if UserAgent("") != nil {
		t.Fatal("empty user agent is null")
	}
	long := strings.Repeat("é", MaxUserAgentChars+10)
	if got := *UserAgent(long); len([]rune(got)) != MaxUserAgentChars {
		t.Fatalf("user agent %d chars", len([]rune(got)))
	}
	if got := Path("/" + strings.Repeat("a", MaxPathChars+10)); len(got) != MaxPathChars {
		t.Fatalf("path %d chars", len(got))
	}
	if ClientIP("203.0.113.7:4312") != "203.0.113.7" || ClientIP("[::1]:80") != "::1" || ClientIP("pipe") != "pipe" {
		t.Fatal("ClientIP")
	}
	if Timestamp(time.Date(2026, 10, 9, 18, 30, 0, 123_456_789, time.FixedZone("x", 3600))) != "2026-10-09T17:30:00.123Z" {
		t.Fatal("Timestamp")
	}
}

func scrape(reg *metrics.Registry) string {
	w := httptest.NewRecorder()
	reg.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	return w.Body.String()
}

func TestParseCreated(t *testing.T) {
	for _, ok := range []string{"01ja2z6q3y8d5v2k9n4r7t1w0x", "ver_01ja2z6r", strings.Repeat("a", MaxCreatedLen)} {
		if id, good := ParseCreated([]string{ok}); !good || *id != ok {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range [][]string{
		{""}, {"a/b"}, {"a b"}, {"a?b"}, {"a#b"}, {"."}, {".."}, {"é"}, {"a\x00"},
		{strings.Repeat("a", MaxCreatedLen+1)}, {"a", "b"},
	} {
		if _, good := ParseCreated(bad); good {
			t.Errorf("%q accepted", bad)
		}
	}
	created := "01jbx9x9b1c3d5e7f9g1h3j5k7"
	b, _ := json.Marshal(OutcomeFor(&State{Forwarded: true, UpstreamStatus: 201, CreatedID: &created}, 201, 2))
	if string(b) != `{"outcome":"responded","status":201,"response_bytes":2,"details":null,"created_id":"01jbx9x9b1c3d5e7f9g1h3j5k7"}` {
		t.Fatalf("%s", b)
	}
	// Only a response names what it created.
	b, _ = json.Marshal(OutcomeFor(&State{Forwarded: true, CreatedID: &created}, 502, 2))
	if !strings.Contains(string(b), `"created_id":null`) {
		t.Fatalf("%s", b)
	}
}
