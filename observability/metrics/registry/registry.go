package metrics

import "sync"

type Registry struct {
	mu       sync.RWMutex
	counters map[string]int64
	gauges   map[string]float64
}

func NewRegistry() *Registry {
	return &Registry{counters: map[string]int64{}, gauges: map[string]float64{}}
}

func (r *Registry) Inc(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[name]++
}

func (r *Registry) Set(name string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[name] = value
}
