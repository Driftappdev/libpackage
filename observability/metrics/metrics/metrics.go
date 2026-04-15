package metrics

// Snapshot is a read-only view of metric values.
type Snapshot struct {
	Counters map[string]int64
	Gauges   map[string]float64
}

// Snapshot returns current metric values.
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
	return Snapshot{Counters: counters, Gauges: gauges}
}
