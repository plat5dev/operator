// Package metrics serves counters, gauges, and histograms in Prometheus text format.
// This repo reports a handful of series (docs/audit.md#metrics), not enough to need a client library.
package metrics

import (
	"bufio"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry is the set of metrics one process serves on /metrics.
type Registry struct {
	mu      sync.Mutex
	metrics []metric
}

type metric interface {
	write(w *bufio.Writer)
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) add(m metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = append(r.metrics, m)
}

// ServeHTTP writes every metric in registration order.
func (r *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	bw := bufio.NewWriter(w)
	r.mu.Lock()
	ms := append([]metric(nil), r.metrics...)
	r.mu.Unlock()
	for _, m := range ms {
		m.write(bw)
	}
	_ = bw.Flush()
}

// Counter is a monotonically increasing value per label set.
type Counter struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	values     map[string]*counterSeries
}

type counterSeries struct {
	labels []string
	v      float64
}

// Counter registers a counter with these label names.
func (r *Registry) Counter(name, help string, labels ...string) *Counter {
	c := &Counter{name: name, help: help, labels: labels, values: map[string]*counterSeries{}}
	r.add(c)
	return c
}

func (c *Counter) Inc(values ...string) { c.Add(1, values...) }

// Add adds v. Adding 0 makes a label set visible before it first happens.
func (c *Counter) Add(v float64, values ...string) {
	if len(values) != len(c.labels) {
		panic(fmt.Sprintf("metrics: %s takes %d labels, got %d", c.name, len(c.labels), len(values)))
	}
	key := strings.Join(values, "\xff")
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.values[key]
	if s == nil {
		s = &counterSeries{labels: append([]string(nil), values...)}
		c.values[key] = s
	}
	s.v += v
}

// Value is the current value for a label set. For tests.
func (c *Counter) Value(values ...string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := c.values[strings.Join(values, "\xff")]; s != nil {
		return s.v
	}
	return 0
}

func (c *Counter) write(w *bufio.Writer) {
	header(w, c.name, c.help, "counter")
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range sortedKeys(c.values) {
		s := c.values[key]
		sample(w, c.name, c.labels, s.labels, nil, s.v)
	}
}

// Gauge is a value that goes up and down.
type Gauge struct {
	name, help string
	mu         sync.Mutex
	v          float64
	f          func() float64
}

// Gauge registers a gauge set with Set.
func (r *Registry) Gauge(name, help string) *Gauge {
	g := &Gauge{name: name, help: help}
	r.add(g)
	return g
}

// GaugeFunc registers a gauge read from f at each scrape.
func (r *Registry) GaugeFunc(name, help string, f func() float64) {
	r.add(&Gauge{name: name, help: help, f: f})
}

func (g *Gauge) Set(v float64) {
	g.mu.Lock()
	g.v = v
	g.mu.Unlock()
}

func (g *Gauge) write(w *bufio.Writer) {
	header(w, g.name, g.help, "gauge")
	v := 0.0
	if g.f != nil {
		v = g.f()
	} else {
		g.mu.Lock()
		v = g.v
		g.mu.Unlock()
	}
	sample(w, g.name, nil, nil, nil, v)
}

// Histogram counts observations into cumulative buckets per label set.
type Histogram struct {
	name, help string
	labels     []string
	buckets    []float64
	mu         sync.Mutex
	values     map[string]*histogramSeries
}

type histogramSeries struct {
	labels []string
	counts []uint64
	sum    float64
	count  uint64
}

// Histogram registers a histogram. buckets are upper bounds in increasing order; +Inf is implied.
func (r *Registry) Histogram(name, help string, buckets []float64, labels ...string) *Histogram {
	h := &Histogram{name: name, help: help, labels: labels, buckets: buckets, values: map[string]*histogramSeries{}}
	r.add(h)
	return h
}

func (h *Histogram) Observe(v float64, values ...string) {
	if len(values) != len(h.labels) {
		panic(fmt.Sprintf("metrics: %s takes %d labels, got %d", h.name, len(h.labels), len(values)))
	}
	key := strings.Join(values, "\xff")
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.values[key]
	if s == nil {
		s = &histogramSeries{labels: append([]string(nil), values...), counts: make([]uint64, len(h.buckets))}
		h.values[key] = s
	}
	for i, b := range h.buckets {
		if v <= b {
			s.counts[i]++
		}
	}
	s.sum += v
	s.count++
}

func (h *Histogram) write(w *bufio.Writer) {
	header(w, h.name, h.help, "histogram")
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, key := range sortedKeys(h.values) {
		s := h.values[key]
		for i, b := range h.buckets {
			sample(w, h.name+"_bucket", h.labels, s.labels, []string{"le", formatFloat(b)}, float64(s.counts[i]))
		}
		sample(w, h.name+"_bucket", h.labels, s.labels, []string{"le", "+Inf"}, float64(s.count))
		sample(w, h.name+"_sum", h.labels, s.labels, nil, s.sum)
		sample(w, h.name+"_count", h.labels, s.labels, nil, float64(s.count))
	}
}

func header(w *bufio.Writer, name, help, typ string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, strings.NewReplacer(`\`, `\\`, "\n", `\n`).Replace(help), name, typ)
}

// sample writes one line. extra is one more name/value pair, such as le.
func sample(w *bufio.Writer, name string, names, values, extra []string, v float64) {
	w.WriteString(name)
	if len(names) > 0 || len(extra) > 0 {
		w.WriteByte('{')
		for i, n := range names {
			if i > 0 {
				w.WriteByte(',')
			}
			fmt.Fprintf(w, `%s="%s"`, n, escape(values[i]))
		}
		if len(extra) > 0 {
			if len(names) > 0 {
				w.WriteByte(',')
			}
			fmt.Fprintf(w, `%s="%s"`, extra[0], escape(extra[1]))
		}
		w.WriteByte('}')
	}
	w.WriteByte(' ')
	w.WriteString(formatFloat(v))
	w.WriteByte('\n')
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escape(s string) string { return labelEscaper.Replace(s) }

func formatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
