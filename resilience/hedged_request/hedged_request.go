// Package flowguard provides enterprise-grade traffic management primitives
// for distributed microservice architectures.
package flowguard

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// HedgedRequest implements the hedged requests pattern for latency tail reduction.
// It sends duplicate requests after a configurable delay and returns the first
// successful response, canceling all other in-flight requests.
//
// Based on "The Tail at Scale" (Dean & Barroso, Google, 2013).
//
// Example:
//
//	h := NewHedgedRequest(HedgedRequestConfig{
//	    HedgeDelay:   5 * time.Millisecond,
//	    MaxHedges:    2,
//	    MinSampleSize: 100,
//	})
//	result, err := h.Do(ctx, func(ctx context.Context) (any, error) {
//	    return client.Fetch(ctx, req)
//	})
type HedgedRequest struct {
	cfg     HedgedRequestConfig
	metrics *hedgeMetrics
	latency *latencyPercentile
	mu      sync.RWMutex
}

// HedgedRequestConfig holds configuration for hedged requests.
type HedgedRequestConfig struct {
	// HedgeDelay is the static delay before sending a hedge request.
	// If AdaptiveHedge is true, this is used as a floor.
	HedgeDelay time.Duration

	// MaxHedges is the maximum number of additional hedge requests (total = MaxHedges+1).
	MaxHedges int

	// AdaptiveHedge enables dynamic hedge delay based on observed P95 latency.
	AdaptiveHedge bool

	// HedgeDelayPercentile is the latency percentile used to set adaptive hedge delay.
	// Default: 95 (P95).
	HedgeDelayPercentile float64

	// MinSampleSize is the minimum number of samples before adaptive mode activates.
	MinSampleSize int

	// HedgeBudget is the maximum fraction of requests that can be hedges (0.0–1.0).
	// Default: 0.1 (10%). Prevents hedge storms under high load.
	HedgeBudget float64

	// OnHedgeSent is called when a hedge request is dispatched (optional, for observability).
	OnHedgeSent func(hedgeIndex int, delay time.Duration)

	// OnHedgeWon is called when a hedge wins the race (optional).
	OnHedgeWon func(hedgeIndex int, latency time.Duration)
}

func (c *HedgedRequestConfig) setDefaults() {
	if c.HedgeDelay == 0 {
		c.HedgeDelay = 5 * time.Millisecond
	}
	if c.MaxHedges == 0 {
		c.MaxHedges = 1
	}
	if c.HedgeDelayPercentile == 0 {
		c.HedgeDelayPercentile = 95
	}
	if c.MinSampleSize == 0 {
		c.MinSampleSize = 100
	}
	if c.HedgeBudget == 0 {
		c.HedgeBudget = 0.10
	}
}

type hedgeMetrics struct {
	totalRequests int64
	hedgesSent    int64
	hedgesWon     int64
	errors        int64
}

func (m *hedgeMetrics) HedgeRate() float64 {
	total := atomic.LoadInt64(&m.totalRequests)
	if total == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&m.hedgesSent)) / float64(total)
}

// HedgedResult carries the response from the winning request.
type HedgedResult struct {
	Value      any
	HedgeIndex int           // 0 = original, >0 = hedge number
	Latency    time.Duration
}

// NewHedgedRequest creates a new HedgedRequest with the given configuration.
func NewHedgedRequest(cfg HedgedRequestConfig) *HedgedRequest {
	cfg.setDefaults()
	return &HedgedRequest{
		cfg:     cfg,
		metrics: &hedgeMetrics{},
		latency: newLatencyPercentile(10_000),
	}
}

// Do executes fn with the hedged request pattern.
// It returns the first non-error result or the last error if all attempts fail.
func (h *HedgedRequest) Do(ctx context.Context, fn func(ctx context.Context) (any, error)) (*HedgedResult, error) {
	atomic.AddInt64(&h.metrics.totalRequests, 1)

	type attempt struct {
		index   int
		value   any
		err     error
		latency time.Duration
	}

	resultCh := make(chan attempt, h.cfg.MaxHedges+1)
	rootCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	launch := func(idx int) {
		start := time.Now()
		val, err := fn(rootCtx)
		dur := time.Since(start)
		resultCh <- attempt{index: idx, value: val, err: err, latency: dur}
	}

	// Launch original request.
	go launch(0)

	// Schedule hedge requests.
	var wg sync.WaitGroup
	for i := 1; i <= h.cfg.MaxHedges; i++ {
		if !h.canHedge() {
			break
		}
		delay := h.hedgeDelay(i)
		wg.Add(1)
		go func(hedgeIdx int, d time.Duration) {
			defer wg.Done()
			select {
			case <-time.After(d):
				atomic.AddInt64(&h.metrics.hedgesSent, 1)
				if h.cfg.OnHedgeSent != nil {
					h.cfg.OnHedgeSent(hedgeIdx, d)
				}
				go launch(hedgeIdx)
			case <-rootCtx.Done():
			}
		}(i, delay)
	}

	// Wait for first success, then cancel remaining.
	var lastErr error
	received := 0
	maxAttempts := h.cfg.MaxHedges + 1

	for received < maxAttempts {
		select {
		case a := <-resultCh:
			received++
			h.latency.record(float64(a.latency.Milliseconds()))
			if a.err == nil {
				cancel() // Cancel remaining in-flight requests.
				if a.index > 0 {
					atomic.AddInt64(&h.metrics.hedgesWon, 1)
					if h.cfg.OnHedgeWon != nil {
						h.cfg.OnHedgeWon(a.index, a.latency)
					}
				}
				return &HedgedResult{Value: a.value, HedgeIndex: a.index, Latency: a.latency}, nil
			}
			lastErr = a.err
			atomic.AddInt64(&h.metrics.errors, 1)
		case <-ctx.Done():
			return nil, fmt.Errorf("hedged request: context canceled: %w", ctx.Err())
		}
	}

	return nil, fmt.Errorf("hedged request: all %d attempts failed: %w", maxAttempts, lastErr)
}

// canHedge checks the hedge budget to prevent hedge storms.
func (h *HedgedRequest) canHedge() bool {
	return h.metrics.HedgeRate() < h.cfg.HedgeBudget
}

// hedgeDelay returns the delay before dispatching the i-th hedge.
func (h *HedgedRequest) hedgeDelay(i int) time.Duration {
	if !h.cfg.AdaptiveHedge || h.latency.count() < h.cfg.MinSampleSize {
		// Apply small jitter to avoid thundering herd.
		jitter := time.Duration(rand.Int63n(int64(h.cfg.HedgeDelay / 10))) //nolint:gosec
		return h.cfg.HedgeDelay*time.Duration(i) + jitter
	}
	p := h.latency.percentile(h.cfg.HedgeDelayPercentile)
	delay := time.Duration(p) * time.Millisecond
	if delay < h.cfg.HedgeDelay {
		delay = h.cfg.HedgeDelay
	}
	return delay * time.Duration(i)
}

// Metrics returns a snapshot of hedge metrics.
func (h *HedgedRequest) Metrics() HedgedMetricsSnapshot {
	return HedgedMetricsSnapshot{
		TotalRequests: atomic.LoadInt64(&h.metrics.totalRequests),
		HedgesSent:    atomic.LoadInt64(&h.metrics.hedgesSent),
		HedgesWon:     atomic.LoadInt64(&h.metrics.hedgesWon),
		Errors:        atomic.LoadInt64(&h.metrics.errors),
		HedgeRate:     h.metrics.HedgeRate(),
		P50LatencyMs:  h.latency.percentile(50),
		P95LatencyMs:  h.latency.percentile(95),
		P99LatencyMs:  h.latency.percentile(99),
	}
}

// HedgedMetricsSnapshot is a point-in-time snapshot of hedge metrics.
type HedgedMetricsSnapshot struct {
	TotalRequests int64
	HedgesSent    int64
	HedgesWon     int64
	Errors        int64
	HedgeRate     float64
	P50LatencyMs  float64
	P95LatencyMs  float64
	P99LatencyMs  float64
}

// -------- latency percentile tracker (t-digest approximation) --------

// latencyPercentile is a thread-safe, fixed-size reservoir sampler
// for computing approximate percentiles.
type latencyPercentile struct {
	mu      sync.RWMutex
	samples []float64
	size    int
	n       int64 // total observations
}

func newLatencyPercentile(reservoirSize int) *latencyPercentile {
	return &latencyPercentile{samples: make([]float64, 0, reservoirSize), size: reservoirSize}
}

func (l *latencyPercentile) record(ms float64) {
	n := atomic.AddInt64(&l.n, 1)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) < l.size {
		l.samples = append(l.samples, ms)
	} else {
		// Reservoir sampling: replace with decreasing probability.
		idx := rand.Int63n(n) //nolint:gosec
		if int(idx) < l.size {
			l.samples[idx] = ms
		}
	}
}

func (l *latencyPercentile) count() int {
	return int(atomic.LoadInt64(&l.n))
}

func (l *latencyPercentile) percentile(p float64) float64 {
	l.mu.RLock()
	s := make([]float64, len(l.samples))
	copy(s, l.samples)
	l.mu.RUnlock()

	if len(s) == 0 {
		return 0
	}
	sortFloat64s(s)
	idx := int(float64(len(s)-1) * p / 100)
	return s[idx]
}

// sortFloat64s sorts a float64 slice in-place (insertion sort for small slices,
// quicksort otherwise).
func sortFloat64s(a []float64) {
	if len(a) < 32 {
		for i := 1; i < len(a); i++ {
			key := a[i]
			j := i - 1
			for j >= 0 && a[j] > key {
				a[j+1] = a[j]
				j--
			}
			a[j+1] = key
		}
		return
	}
	quickSortFloat64(a, 0, len(a)-1)
}

func quickSortFloat64(a []float64, lo, hi int) {
	if lo >= hi {
		return
	}
	pivot := a[lo+(hi-lo)/2]
	i, j := lo, hi
	for i <= j {
		for a[i] < pivot {
			i++
		}
		for a[j] > pivot {
			j--
		}
		if i <= j {
			a[i], a[j] = a[j], a[i]
			i++
			j--
		}
	}
	quickSortFloat64(a, lo, j)
	quickSortFloat64(a, i, hi)
}

// ErrAllHedgesFailed is returned when every attempt (original + hedges) fails.
var ErrAllHedgesFailed = errors.New("flowguard: all hedged requests failed")
