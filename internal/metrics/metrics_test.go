package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExposition(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("writes_total", "Writes by result.", "write", "result")
	c.Add(0, "intent", "failed")
	c.Inc("intent", "ok")
	c.Inc("intent", "ok")
	c.Inc("outcome", `a"b\c`)
	r.GaugeFunc("queue", "Waiting.", func() float64 { return 3 })
	g := r.Gauge("stale", "Stale.")
	g.Set(1.5)
	h := r.Histogram("duration_seconds", "Duration.", []float64{0.1, 1}, "result")
	h.Observe(0.0625, "ok")
	h.Observe(0.5, "ok")
	h.Observe(4, "ok")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("content type %q", w.Header().Get("Content-Type"))
	}
	want := `# HELP writes_total Writes by result.
# TYPE writes_total counter
writes_total{write="intent",result="failed"} 0
writes_total{write="intent",result="ok"} 2
writes_total{write="outcome",result="a\"b\\c"} 1
# HELP queue Waiting.
# TYPE queue gauge
queue 3
# HELP stale Stale.
# TYPE stale gauge
stale 1.5
# HELP duration_seconds Duration.
# TYPE duration_seconds histogram
duration_seconds_bucket{result="ok",le="0.1"} 1
duration_seconds_bucket{result="ok",le="1"} 2
duration_seconds_bucket{result="ok",le="+Inf"} 3
duration_seconds_sum{result="ok"} 4.5625
duration_seconds_count{result="ok"} 3
`
	if got := w.Body.String(); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if c.Value("intent", "ok") != 2 || c.Value("nope", "x") != 0 {
		t.Fatal("Value")
	}
}

func TestLabelCountMismatchPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	NewRegistry().Counter("c", "c", "a").Inc()
}
