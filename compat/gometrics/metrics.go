// Package gometrics provides production-grade application metrics:
// counters, gauges, histograms, summaries, timers, and a Prometheus-compatible
// exposition format — all with zero external dependencies.
package gometrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---- Registry ---------------------------------------------------------------

// Registry holds all registered metrics.
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]Metric
}

// DefaultRegistry is the package-level default registry.
var DefaultRegistry = NewRegistry()

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{metrics: make(map[string]Metric)}
}

// Register adds a metric to the registry.
func (r *Registry) Register(m Metric) {
	r.mu.Lock()
	r.metrics[m.Desc().Name] = m
	r.mu.Unlock()
}

// Get returns a registered metric by name.
func (r *Registry) Get(name string) (Metric, bool) {
	r.mu.RLock()
	m, ok := r.metrics[name]
	r.mu.RUnlock()
	return m, ok
}

// All returns a snapshot of all registered metrics sorted by name.
func (r *Registry) All() []Metric {
	r.mu.RLock()
	out := make([]Metric, 0, len(r.metrics))
	for _, m := range r.metrics {
		out = append(out, m)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Desc().Name < out[j].Desc().Name })
	return out
}

// WritePrometheus writes all metrics in Prometheus text format to w.
func (r *Registry) WritePrometheus(w io.Writer) {
	for _, m := range r.All() {
		m.WritePrometheus(w)
	}
}

// Handler returns an http.HandlerFunc serving Prometheus metrics.
func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		r.WritePrometheus(w)
	}
}

// ---- Metric interface -------------------------------------------------------

// Desc describes a metric.
type Desc struct {
	Name   string
	Help   string
	Labels []string
}

// Metric is the interface all metric types implement.
type Metric interface {
	Desc() Desc
	WritePrometheus(w io.Writer)
}

// labelStr formats label pairs as a Prometheus label string.
func labelStr(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, fmt.Sprintf(`%s=%q`, k, v))
	}
	sort.Strings(pairs)
	return "{" + strings.Join(pairs, ",") + "}"
}

// ---- Counter ----------------------------------------------------------------

// Counter is a monotonically increasing integer counter.
type Counter struct {
	desc  Desc
	mu    sync.RWMutex
	vals  map[string]*atomic.Int64
	label map[string]map[string]string
}

// NewCounter registers and returns a Counter.
func NewCounter(name, help string, labels ...string) *Counter {
	c := &Counter{
		desc:  Desc{Name: name, Help: help, Labels: labels},
		vals:  make(map[string]*atomic.Int64),
		label: make(map[string]map[string]string),
	}
	DefaultRegistry.Register(c)
	return c
}

func (c *Counter) Desc() Desc { return c.desc }

// Inc increments the counter by 1 for the given label values.
func (c *Counter) Inc(labelVals ...string) { c.Add(1, labelVals...) }

// Add adds n to the counter.
func (c *Counter) Add(n int64, labelVals ...string) {
	key, lblMap := c.keyFor(labelVals)
	c.mu.Lock()
	if _, ok := c.vals[key]; !ok {
		c.vals[key] = &atomic.Int64{}
		c.label[key] = lblMap
	}
	c.mu.Unlock()
	c.vals[key].Add(n)
}

// Value returns the current counter value.
func (c *Counter) Value(labelVals ...string) int64 {
	key, _ := c.keyFor(labelVals)
	c.mu.RLock()
	v, ok := c.vals[key]
	c.mu.RUnlock()
	if !ok {
		return 0
	}
	return v.Load()
}

func (c *Counter) keyFor(vals []string) (string, map[string]string) {
	m := make(map[string]string, len(c.desc.Labels))
	for i, l := range c.desc.Labels {
		if i < len(vals) {
			m[l] = vals[i]
		}
	}
	return labelStr(m), m
}

func (c *Counter) WritePrometheus(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.desc.Name, c.desc.Help, c.desc.Name)
	c.mu.RLock()
	defer c.mu.RUnlock()
	for key, v := range c.vals {
		fmt.Fprintf(w, "%s%s %d\n", c.desc.Name, key, v.Load())
	}
}

// ---- Gauge ------------------------------------------------------------------

// Gauge is a metric that can go up or down.
type Gauge struct {
	desc  Desc
	mu    sync.RWMutex
	vals  map[string]float64
	label map[string]map[string]string
}

// NewGauge registers and returns a Gauge.
func NewGauge(name, help string, labels ...string) *Gauge {
	g := &Gauge{
		desc:  Desc{Name: name, Help: help, Labels: labels},
		vals:  make(map[string]float64),
		label: make(map[string]map[string]string),
	}
	DefaultRegistry.Register(g)
	return g
}

func (g *Gauge) Desc() Desc { return g.desc }

// Set sets the gauge to v.
func (g *Gauge) Set(v float64, labelVals ...string) {
	key, lm := g.keyFor(labelVals)
	g.mu.Lock()
	g.vals[key] = v
	g.label[key] = lm
	g.mu.Unlock()
}

// Inc increments by 1.
func (g *Gauge) Inc(labelVals ...string) { g.Add(1, labelVals...) }

// Dec decrements by 1.
func (g *Gauge) Dec(labelVals ...string) { g.Add(-1, labelVals...) }

// Add adds delta to the gauge.
func (g *Gauge) Add(delta float64, labelVals ...string) {
	key, lm := g.keyFor(labelVals)
	g.mu.Lock()
	g.vals[key] += delta
	g.label[key] = lm
	g.mu.Unlock()
}

// Value returns the current gauge value.
func (g *Gauge) Value(labelVals ...string) float64 {
	key, _ := g.keyFor(labelVals)
	g.mu.RLock()
	v := g.vals[key]
	g.mu.RUnlock()
	return v
}

func (g *Gauge) keyFor(vals []string) (string, map[string]string) {
	m := make(map[string]string, len(g.desc.Labels))
	for i, l := range g.desc.Labels {
		if i < len(vals) {
			m[l] = vals[i]
		}
	}
	return labelStr(m), m
}

func (g *Gauge) WritePrometheus(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.desc.Name, g.desc.Help, g.desc.Name)
	g.mu.RLock()
	defer g.mu.RUnlock()
	for key, v := range g.vals {
		fmt.Fprintf(w, "%s%s %g\n", g.desc.Name, key, v)
	}
}

// ---- Histogram --------------------------------------------------------------

// Histogram tracks value distributions in configurable buckets.
type Histogram struct {
	desc    Desc
	buckets []float64
	mu      sync.Mutex
	series  map[string]*histSeries
}

type histSeries struct {
	counts []uint64
	sum    float64
	total  uint64
	labels map[string]string
}

// NewHistogram registers a Histogram with the given bucket boundaries.
func NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	h := &Histogram{
		desc:    Desc{Name: name, Help: help, Labels: labels},
		buckets: sorted,
		series:  make(map[string]*histSeries),
	}
	DefaultRegistry.Register(h)
	return h
}

// DefBuckets are reasonable default histogram buckets (latency in seconds).
var DefBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

func (h *Histogram) Desc() Desc { return h.desc }

// Observe records a single value.
func (h *Histogram) Observe(v float64, labelVals ...string) {
	key, lm := h.keyFor(labelVals)
	h.mu.Lock()
	s, ok := h.series[key]
	if !ok {
		s = &histSeries{counts: make([]uint64, len(h.buckets)+1), labels: lm}
		h.series[key] = s
	}
	s.sum += v
	s.total++
	for i, b := range h.buckets {
		if v <= b {
			s.counts[i]++
		}
	}
	s.counts[len(h.buckets)]++ // +Inf
	h.mu.Unlock()
}

func (h *Histogram) keyFor(vals []string) (string, map[string]string) {
	m := make(map[string]string, len(h.desc.Labels))
	for i, l := range h.desc.Labels {
		if i < len(vals) {
			m[l] = vals[i]
		}
	}
	return labelStr(m), m
}

func (h *Histogram) WritePrometheus(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.desc.Name, h.desc.Help, h.desc.Name)
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, s := range h.series {
		lbl := strings.TrimSuffix(strings.TrimPrefix(key, "{"), "}")
		addlbl := func(extra string) string {
			if lbl == "" {
				return "{" + extra + "}"
			}
			return "{" + lbl + "," + extra + "}"
		}
		for i, b := range h.buckets {
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.desc.Name, addlbl(fmt.Sprintf(`le="%g"`, b)), s.counts[i])
		}
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.desc.Name, addlbl(`le="+Inf"`), s.counts[len(h.buckets)])
		fmt.Fprintf(w, "%s_sum%s %g\n", h.desc.Name, key, s.sum)
		fmt.Fprintf(w, "%s_count%s %d\n", h.desc.Name, key, s.total)
	}
}

// ---- Timer ------------------------------------------------------------------

// Timer measures elapsed time and records to a Histogram.
type Timer struct {
	hist      *Histogram
	labelVals []string
	start     time.Time
}

// NewTimer starts a timer that will record to h when Stop is called.
func NewTimer(h *Histogram, labelVals ...string) *Timer {
	return &Timer{hist: h, labelVals: labelVals, start: time.Now()}
}

// ObserveDuration records elapsed seconds since the timer was created.
func (t *Timer) ObserveDuration() {
	t.hist.Observe(time.Since(t.start).Seconds(), t.labelVals...)
}

// ---- Summary ----------------------------------------------------------------

// Summary tracks quantile estimates using a sliding reservoir.
type Summary struct {
	desc       Desc
	quantiles  []float64
	windowSize int
	mu         sync.Mutex
	series     map[string]*sumSeries
}

type sumSeries struct {
	samples []float64
	sum     float64
	count   uint64
	labels  map[string]string
}

// NewSummary registers a Summary with the given quantiles (e.g. 0.5, 0.95, 0.99).
func NewSummary(name, help string, quantiles []float64, labels ...string) *Summary {
	s := &Summary{
		desc:       Desc{Name: name, Help: help, Labels: labels},
		quantiles:  quantiles,
		windowSize: 1000,
		series:     make(map[string]*sumSeries),
	}
	DefaultRegistry.Register(s)
	return s
}

func (s *Summary) Desc() Desc { return s.desc }

// Observe records a value.
func (s *Summary) Observe(v float64, labelVals ...string) {
	key, lm := s.keyFor(labelVals)
	s.mu.Lock()
	ser, ok := s.series[key]
	if !ok {
		ser = &sumSeries{labels: lm}
		s.series[key] = ser
	}
	ser.samples = append(ser.samples, v)
	if len(ser.samples) > s.windowSize {
		ser.samples = ser.samples[len(ser.samples)-s.windowSize:]
	}
	ser.sum += v
	ser.count++
	s.mu.Unlock()
}

func (s *Summary) keyFor(vals []string) (string, map[string]string) {
	m := make(map[string]string, len(s.desc.Labels))
	for i, l := range s.desc.Labels {
		if i < len(vals) {
			m[l] = vals[i]
		}
	}
	return labelStr(m), m
}

func (s *Summary) WritePrometheus(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s summary\n", s.desc.Name, s.desc.Help, s.desc.Name)
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, ser := range s.series {
		sorted := make([]float64, len(ser.samples))
		copy(sorted, ser.samples)
		sort.Float64s(sorted)
		lbl := strings.TrimSuffix(strings.TrimPrefix(key, "{"), "}")
		addlbl := func(extra string) string {
			if lbl == "" {
				return "{" + extra + "}"
			}
			return "{" + lbl + "," + extra + "}"
		}
		for _, q := range s.quantiles {
			val := quantileValue(sorted, q)
			fmt.Fprintf(w, "%s%s %g\n", s.desc.Name, addlbl(fmt.Sprintf(`quantile="%g"`, q)), val)
		}
		fmt.Fprintf(w, "%s_sum%s %g\n", s.desc.Name, key, ser.sum)
		fmt.Fprintf(w, "%s_count%s %d\n", s.desc.Name, key, ser.count)
	}
}

func quantileValue(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	idx := q * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (idx-float64(lo))*(sorted[hi]-sorted[lo])
}

// ---- Convenience package-level functions ------------------------------------

// MustCounter creates a counter in the default registry (panics on duplicate name).
func MustCounter(name, help string, labels ...string) *Counter {
	return NewCounter(name, help, labels...)
}

// MustGauge creates a gauge in the default registry.
func MustGauge(name, help string, labels ...string) *Gauge {
	return NewGauge(name, help, labels...)
}

// MustHistogram creates a histogram in the default registry.
func MustHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	return NewHistogram(name, help, buckets, labels...)
}

// Handler returns the default registry's Prometheus HTTP handler.
func Handler() http.HandlerFunc {
	return DefaultRegistry.Handler()
}
