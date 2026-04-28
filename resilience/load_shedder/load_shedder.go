package resilience

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ShedStrategy determines how the load shedder selects which requests to drop.
type ShedStrategy int

const (
	// ShedRandom drops requests randomly with probability = load fraction.
	ShedRandom ShedStrategy = iota
	// ShedAll rejects all requests when the shedder is active.
	ShedAll
	// ShedPriority drops requests below a minimum priority threshold.
	ShedPriority
	// ShedLatency sheds when estimated latency exceeds a target.
	ShedLatency
)

// LoadShedderConfig configures a LoadShedder.
type LoadShedderConfig struct {
	// Name identifies the shedder.
	Name string
	// Strategy selects the shedding algorithm.
	Strategy ShedStrategy
	// CPUThreshold (0–100): shed when CPU% exceeds this (requires CPUProbe).
	CPUThreshold float64
	// QueueThreshold: shed when queue depth exceeds this.
	QueueThreshold int
	// LatencyTarget: shed when moving-average latency exceeds this (ShedLatency).
	LatencyTarget time.Duration
	// MinPriority: requests with priority < this are shed (ShedPriority).
	MinPriority int
	// CPUProbe returns current CPU utilization (0–100). Optional.
	CPUProbe func() float64
	// QueueProbe returns current queue depth. Optional.
	QueueProbe func() int
	// OnShed is called whenever a request is shed.
	OnShed func(name, reason string)
	// OnAccept is called whenever a request is accepted.
	OnAccept func(name string)
	// CooldownPeriod: min time between shedding state changes.
	CooldownPeriod time.Duration
}

// LoadShedder protects a service by proactively rejecting excess requests
// when system resources are saturated. Implements multiple strategies.
type LoadShedder struct {
	cfg     LoadShedderConfig
	rng     *rand.Rand
	mu      sync.RWMutex
	active  bool
	lastChg time.Time

	// latency tracking (EWMA)
	latencyEWMA  float64
	ewmaAlpha    float64

	// metrics
	accepted atomic.Int64
	shed     atomic.Int64
	total    atomic.Int64
	activations atomic.Int64
}

// NewLoadShedder creates and returns a LoadShedder.
func NewLoadShedder(cfg LoadShedderConfig) *LoadShedder {
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	if cfg.CooldownPeriod <= 0 {
		cfg.CooldownPeriod = 500 * time.Millisecond
	}
	return &LoadShedder{
		cfg:       cfg,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
		ewmaAlpha: 0.1, // smoothing factor
	}
}

// Allow determines whether a request should be processed.
// priority is only used with ShedPriority strategy.
// Returns nil to proceed, error to shed.
func (s *LoadShedder) Allow(ctx context.Context, priority int) error {
	s.total.Add(1)

	// Evaluate load signals
	shouldShed := s.evaluate(priority)

	if shouldShed {
		s.shed.Add(1)
		reason := s.currentReason(priority)
		if s.cfg.OnShed != nil {
			s.cfg.OnShed(s.cfg.Name, reason)
		}
		return fmt.Errorf("load_shedder[%s]: request shed (%s)", s.cfg.Name, reason)
	}

	s.accepted.Add(1)
	if s.cfg.OnAccept != nil {
		s.cfg.OnAccept(s.cfg.Name)
	}
	return nil
}

// RecordLatency updates the EWMA latency estimate. Call after each request.
func (s *LoadShedder) RecordLatency(d time.Duration) {
	s.mu.Lock()
	ms := float64(d.Milliseconds())
	if s.latencyEWMA == 0 {
		s.latencyEWMA = ms
	} else {
		s.latencyEWMA = s.ewmaAlpha*ms + (1-s.ewmaAlpha)*s.latencyEWMA
	}
	s.mu.Unlock()
}

func (s *LoadShedder) evaluate(priority int) bool {
	s.mu.RLock()
	ewma := s.latencyEWMA
	s.mu.RUnlock()

	switch s.cfg.Strategy {
	case ShedAll:
		return s.isOverloaded(ewma)

	case ShedRandom:
		if !s.isOverloaded(ewma) {
			return false
		}
		// Random drop probability proportional to excess load
		prob := s.loadFraction(ewma)
		return s.rng.Float64() < prob

	case ShedPriority:
		if !s.isOverloaded(ewma) {
			return false
		}
		return priority < s.cfg.MinPriority

	case ShedLatency:
		if s.cfg.LatencyTarget <= 0 {
			return false
		}
		return time.Duration(ewma)*time.Millisecond > s.cfg.LatencyTarget
	}
	return false
}

func (s *LoadShedder) isOverloaded(ewma float64) bool {
	if s.cfg.CPUThreshold > 0 && s.cfg.CPUProbe != nil {
		if s.cfg.CPUProbe() > s.cfg.CPUThreshold {
			s.setActive(true)
			return true
		}
	}
	if s.cfg.QueueThreshold > 0 && s.cfg.QueueProbe != nil {
		if s.cfg.QueueProbe() > s.cfg.QueueThreshold {
			s.setActive(true)
			return true
		}
	}
	if s.cfg.LatencyTarget > 0 && time.Duration(ewma)*time.Millisecond > s.cfg.LatencyTarget {
		s.setActive(true)
		return true
	}
	s.setActive(false)
	return false
}

func (s *LoadShedder) loadFraction(ewma float64) float64 {
	if s.cfg.LatencyTarget <= 0 {
		return 0.5
	}
	target := float64(s.cfg.LatencyTarget.Milliseconds())
	if ewma <= target {
		return 0
	}
	excess := (ewma - target) / target
	if excess > 1 {
		return 1
	}
	return excess
}

func (s *LoadShedder) setActive(val bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if val == s.active {
		return
	}
	if time.Since(s.lastChg) < s.cfg.CooldownPeriod {
		return
	}
	s.active = val
	s.lastChg = time.Now()
	if val {
		s.activations.Add(1)
	}
}

func (s *LoadShedder) currentReason(priority int) string {
	switch s.cfg.Strategy {
	case ShedAll:
		return "overloaded, shed all"
	case ShedRandom:
		return "random shed (load fraction)"
	case ShedPriority:
		return fmt.Sprintf("low priority %d < %d", priority, s.cfg.MinPriority)
	case ShedLatency:
		s.mu.RLock()
		ewma := s.latencyEWMA
		s.mu.RUnlock()
		return fmt.Sprintf("latency %.0fms > target %s", ewma, s.cfg.LatencyTarget)
	}
	return "unknown"
}

// IsActive returns true if the shedder is currently active.
func (s *LoadShedder) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

// Force manually activates or deactivates load shedding.
func (s *LoadShedder) Force(active bool) {
	s.mu.Lock()
	s.active = active
	s.lastChg = time.Now()
	s.mu.Unlock()
}

// Do is a convenience wrapper that calls Allow before executing fn.
func (s *LoadShedder) Do(ctx context.Context, priority int, fn func() error) error {
	if err := s.Allow(ctx, priority); err != nil {
		return err
	}
	start := time.Now()
	err := fn()
	s.RecordLatency(time.Since(start))
	return err
}

// Stats returns a snapshot.
func (s *LoadShedder) Stats() LoadShedderStats {
	s.mu.RLock()
	ewma := s.latencyEWMA
	active := s.active
	s.mu.RUnlock()
	return LoadShedderStats{
		Name:        s.cfg.Name,
		Total:       s.total.Load(),
		Accepted:    s.accepted.Load(),
		Shed:        s.shed.Load(),
		Activations: s.activations.Load(),
		LatencyEWMA: time.Duration(ewma) * time.Millisecond,
		Active:      active,
	}
}

// LoadShedderStats is a point-in-time snapshot.
type LoadShedderStats struct {
	Name        string
	Total       int64
	Accepted    int64
	Shed        int64
	Activations int64
	LatencyEWMA time.Duration
	Active      bool
}
