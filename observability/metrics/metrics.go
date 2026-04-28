package metrics

import (
	"sort"
	"sync"
)

type Registry struct {
	mu         sync.RWMutex
	counters   map[string]int64
	gauges     map[string]float64
	histograms map[string][]float64
}

func NewRegistry() *Registry {
	return &Registry{counters: map[string]int64{}, gauges: map[string]float64{}, histograms: map[string][]float64{}}
}

func (r *Registry) Inc(name string) { r.Add(name, 1) }
func (r *Registry) Add(name string, delta int64) {
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[name] += delta
}
func (r *Registry) Set(name string, value float64) {
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[name] = value
}
func (r *Registry) Observe(name string, value float64) {
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.histograms[name] = append(r.histograms[name], value)
}

// Snapshot is a read-only view of metric values.
type Snapshot struct {
	Counters   map[string]int64
	Gauges     map[string]float64
	Histograms map[string]HistogramSnapshot
}

type HistogramSnapshot struct {
	Count int
	Sum   float64
	Avg   float64
	Min   float64
	Max   float64
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	counters := make(map[string]int64, len(r.counters))
	for k, v := range r.counters {
		counters[k] = v
	}
	gauges := make(map[string]float64, len(r.gauges))
	for k, v := range r.gauges {
		gauges[k] = v
	}
	hists := make(map[string]HistogramSnapshot, len(r.histograms))
	for k, points := range r.histograms {
		hists[k] = summarize(points)
	}
	return Snapshot{Counters: counters, Gauges: gauges, Histograms: hists}
}

func (r *Registry) Counter(name string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.counters[name]
}

func (r *Registry) Gauge(name string) float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gauges[name]
}

func (r *Registry) MetricNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := map[string]struct{}{}
	for k := range r.counters {
		set[k] = struct{}{}
	}
	for k := range r.gauges {
		set[k] = struct{}{}
	}
	for k := range r.histograms {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = map[string]int64{}
	r.gauges = map[string]float64{}
	r.histograms = map[string][]float64{}
}

func (s Snapshot) MetricNames() []string {
	set := map[string]struct{}{}
	for k := range s.Counters {
		set[k] = struct{}{}
	}
	for k := range s.Gauges {
		set[k] = struct{}{}
	}
	for k := range s.Histograms {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func summarize(values []float64) HistogramSnapshot {
	if len(values) == 0 {
		return HistogramSnapshot{}
	}
	min, max := values[0], values[0]
	sum := 0.0
	for _, v := range values {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return HistogramSnapshot{Count: len(values), Sum: sum, Avg: sum / float64(len(values)), Min: min, Max: max}
}
