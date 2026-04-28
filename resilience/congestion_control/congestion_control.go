package flowguard

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// CongestionController implements Additive Increase / Multiplicative Decrease (AIMD)
// concurrency control inspired by TCP congestion control and Netflix's Concurrency
// Limits library.
//
// It dynamically adjusts the in-flight request limit based on observed latency and
// error rates, preventing overload cascades under high traffic.
//
// Algorithms supported:
//   - AIMD  (classic; aggressive reduction on congestion)
//   - Vegas (gradient-based; smoother, less oscillation)
//   - Gradient2 (Netflix-style; RTT gradient)
//
// Example:
//
//	cc := NewCongestionController(CongestionConfig{
//	    Algorithm:   AlgorithmVegas,
//	    InitLimit:   20,
//	    MinLimit:    5,
//	    MaxLimit:    1000,
//	    Timeout:     500 * time.Millisecond,
//	})
//	token, err := cc.Acquire(ctx)
//	if err != nil { /* shed load */ }
//	defer token.Release(success)
type CongestionController struct {
	cfg     CongestionConfig
	mu      sync.Mutex
	limit   int64
	inflight int64
	metrics *ccMetrics
	rttTracker *rttTracker
	sample  congestionSample
}

// CongestionAlgorithm selects the limit-adjustment algorithm.
type CongestionAlgorithm int

const (
	AlgorithmAIMD      CongestionAlgorithm = iota // Additive Increase / Multiplicative Decrease
	AlgorithmVegas                                // TCP Vegas (RTT gradient)
	AlgorithmGradient2                            // Netflix Gradient2
)

// CongestionConfig configures the CongestionController.
type CongestionConfig struct {
	Algorithm   CongestionAlgorithm
	InitLimit   int64
	MinLimit    int64
	MaxLimit    int64
	Timeout     time.Duration

	// AIMD parameters.
	AIMDIncrease  float64 // additive increase per RTT (default: 1)
	AIMDDecrease  float64 // multiplicative decrease on congestion (default: 0.9)

	// Vegas / Gradient2 parameters.
	// Gradient2ProbeMultiplier controls how aggressively the limit grows.
	Gradient2ProbeMultiplier float64 // default: 2.0
	// SmoothingFactor for EWMA RTT tracking (default: 0.7).
	SmoothingFactor float64

	// IgnoreLatencies prevents latency-based limit adjustment (use error rate only).
	IgnoreLatencies bool

	// OnLimitChange is called when the concurrency limit changes.
	OnLimitChange func(old, new int64, reason string)

	// OnDrop is called when a request is rejected due to limit.
	OnDrop func(inflight, limit int64)
}

func (c *CongestionConfig) setDefaults() {
	if c.InitLimit == 0 {
		c.InitLimit = 20
	}
	if c.MinLimit == 0 {
		c.MinLimit = 1
	}
	if c.MaxLimit == 0 {
		c.MaxLimit = 1024
	}
	if c.Timeout == 0 {
		c.Timeout = 500 * time.Millisecond
	}
	if c.AIMDIncrease == 0 {
		c.AIMDIncrease = 1.0
	}
	if c.AIMDDecrease == 0 {
		c.AIMDDecrease = 0.9
	}
	if c.Gradient2ProbeMultiplier == 0 {
		c.Gradient2ProbeMultiplier = 2.0
	}
	if c.SmoothingFactor == 0 {
		c.SmoothingFactor = 0.7
	}
}

type congestionSample struct {
	successCount int64
	dropCount    int64
	sumRTTNs     int64
	windowStart  time.Time
}

type ccMetrics struct {
	acquired    int64
	dropped     int64
	timeouts    int64
	limitUp     int64
	limitDown   int64
	totalRTTNs  int64
}

type rttTracker struct {
	minRTT    float64 // nanoseconds, EWMA
	currentRTT float64
	alpha     float64 // smoothing factor
	mu        sync.Mutex
	samples   int64
}

func newRTTTracker(alpha float64) *rttTracker {
	return &rttTracker{alpha: alpha, minRTT: math.MaxFloat64}
}

func (t *rttTracker) record(rttNs float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	atomic.AddInt64(&t.samples, 1)
	if t.currentRTT == 0 {
		t.currentRTT = rttNs
	} else {
		t.currentRTT = t.alpha*t.currentRTT + (1-t.alpha)*rttNs
	}
	if rttNs < t.minRTT {
		t.minRTT = rttNs
	}
}

func (t *rttTracker) gradient() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.minRTT == 0 || t.minRTT == math.MaxFloat64 {
		return 1.0
	}
	return t.minRTT / t.currentRTT
}

// CongestionToken represents an acquired concurrency slot.
type CongestionToken struct {
	cc    *CongestionController
	start time.Time
}

// Release returns the token. success=true on normal completion; false on error/timeout.
func (t *CongestionToken) Release(success bool) {
	if t == nil || t.cc == nil {
		return
	}
	rtt := time.Since(t.start)
	atomic.AddInt64(&t.cc.inflight, -1)
	atomic.AddInt64(&t.cc.metrics.totalRTTNs, rtt.Nanoseconds())
	t.cc.rttTracker.record(float64(rtt.Nanoseconds()))
	t.cc.onComplete(success, rtt)
}

// NewCongestionController creates a new CongestionController.
func NewCongestionController(cfg CongestionConfig) *CongestionController {
	cfg.setDefaults()
	return &CongestionController{
		cfg:        cfg,
		limit:      cfg.InitLimit,
		metrics:    &ccMetrics{},
		rttTracker: newRTTTracker(cfg.SmoothingFactor),
		sample:     congestionSample{windowStart: time.Now()},
	}
}

// Acquire requests a concurrency token. Blocks until the limit allows or ctx expires.
func (c *CongestionController) Acquire(ctx context.Context) (*CongestionToken, error) {
	for {
		limit := atomic.LoadInt64(&c.limit)
		inflight := atomic.LoadInt64(&c.inflight)

		if inflight < limit {
			if atomic.CompareAndSwapInt64(&c.inflight, inflight, inflight+1) {
				atomic.AddInt64(&c.metrics.acquired, 1)
				return &CongestionToken{cc: c, start: time.Now()}, nil
			}
			// CAS failed – retry loop.
			continue
		}

		// At limit – try brief wait before shedding.
		atomic.AddInt64(&c.metrics.dropped, 1)
		if c.cfg.OnDrop != nil {
			c.cfg.OnDrop(inflight, limit)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("flowguard: congestion limit reached (%d/%d): %w",
				inflight, limit, ctx.Err())
		case <-time.After(1 * time.Millisecond):
			// Re-check on next iteration.
		}
	}
}

// AcquireOrDrop immediately returns ErrCongestionLimitReached if at capacity.
// Use this for latency-sensitive paths that should shed load instantly.
func (c *CongestionController) AcquireOrDrop() (*CongestionToken, error) {
	limit := atomic.LoadInt64(&c.limit)
	inflight := atomic.LoadInt64(&c.inflight)
	if inflight >= limit {
		atomic.AddInt64(&c.metrics.dropped, 1)
		if c.cfg.OnDrop != nil {
			c.cfg.OnDrop(inflight, limit)
		}
		return nil, ErrCongestionLimitReached
	}
	if atomic.CompareAndSwapInt64(&c.inflight, inflight, inflight+1) {
		atomic.AddInt64(&c.metrics.acquired, 1)
		return &CongestionToken{cc: c, start: time.Now()}, nil
	}
	return nil, ErrCongestionLimitReached
}

// CurrentLimit returns the current concurrency limit.
func (c *CongestionController) CurrentLimit() int64 { return atomic.LoadInt64(&c.limit) }

// Inflight returns the current number of in-flight requests.
func (c *CongestionController) Inflight() int64 { return atomic.LoadInt64(&c.inflight) }

func (c *CongestionController) onComplete(success bool, rtt time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if success {
		atomic.AddInt64(&c.sample.successCount, 1)
	} else {
		atomic.AddInt64(&c.sample.dropCount, 1)
	}
	atomic.AddInt64(&c.sample.sumRTTNs, rtt.Nanoseconds())

	// Adjust limit after every N samples.
	const sampleWindow = 50
	total := atomic.LoadInt64(&c.sample.successCount) + atomic.LoadInt64(&c.sample.dropCount)
	if total < sampleWindow {
		return
	}

	c.adjustLimit()
	// Reset sample window.
	c.sample = congestionSample{windowStart: time.Now()}
}

func (c *CongestionController) adjustLimit() {
	switch c.cfg.Algorithm {
	case AlgorithmAIMD:
		c.adjustAIMD()
	case AlgorithmVegas:
		c.adjustVegas()
	case AlgorithmGradient2:
		c.adjustGradient2()
	}
}

func (c *CongestionController) adjustAIMD() {
	drops := atomic.LoadInt64(&c.sample.dropCount)
	success := atomic.LoadInt64(&c.sample.successCount)
	total := drops + success

	old := atomic.LoadInt64(&c.limit)
	var newLimit int64

	if total > 0 && float64(drops)/float64(total) > 0.05 {
		// Congestion detected: multiplicative decrease.
		newLimit = int64(math.Max(float64(c.cfg.MinLimit),
			math.Floor(float64(old)*c.cfg.AIMDDecrease)))
		atomic.AddInt64(&c.metrics.limitDown, 1)
	} else {
		// No congestion: additive increase.
		newLimit = int64(math.Min(float64(c.cfg.MaxLimit),
			float64(old)+c.cfg.AIMDIncrease))
		atomic.AddInt64(&c.metrics.limitUp, 1)
	}

	if newLimit != old {
		atomic.StoreInt64(&c.limit, newLimit)
		if c.cfg.OnLimitChange != nil {
			c.cfg.OnLimitChange(old, newLimit, "AIMD")
		}
	}
}

func (c *CongestionController) adjustVegas() {
	gradient := c.rttTracker.gradient() // minRTT / currentRTT
	old := atomic.LoadInt64(&c.limit)

	var newLimit int64
	const vegasTolerance = 0.9

	if gradient < vegasTolerance {
		// RTT is increasing: reduce limit.
		newLimit = int64(math.Max(float64(c.cfg.MinLimit),
			math.Floor(float64(old)*gradient)))
		atomic.AddInt64(&c.metrics.limitDown, 1)
	} else {
		// Headroom available: grow.
		newLimit = int64(math.Min(float64(c.cfg.MaxLimit), float64(old)+1))
		atomic.AddInt64(&c.metrics.limitUp, 1)
	}

	if newLimit != old {
		atomic.StoreInt64(&c.limit, newLimit)
		if c.cfg.OnLimitChange != nil {
			c.cfg.OnLimitChange(old, newLimit, "Vegas")
		}
	}
}

func (c *CongestionController) adjustGradient2() {
	gradient := c.rttTracker.gradient()
	old := atomic.LoadInt64(&c.limit)
	inflight := atomic.LoadInt64(&c.inflight)

	// Netflix Gradient2: newLimit = gradient * limit + sqrt(limit)
	probe := math.Sqrt(float64(old)) * c.cfg.Gradient2ProbeMultiplier
	newF := gradient*float64(old) + probe

	// Don't grow if we have headroom (inflight << limit).
	if float64(inflight) < float64(old)*0.5 {
		newF = float64(old) // stable
	}

	newLimit := int64(math.Max(float64(c.cfg.MinLimit), math.Min(float64(c.cfg.MaxLimit), newF)))

	if newLimit > old {
		atomic.AddInt64(&c.metrics.limitUp, 1)
	} else if newLimit < old {
		atomic.AddInt64(&c.metrics.limitDown, 1)
	}

	if newLimit != old {
		atomic.StoreInt64(&c.limit, newLimit)
		if c.cfg.OnLimitChange != nil {
			c.cfg.OnLimitChange(old, newLimit, "Gradient2")
		}
	}
}

// CCMetricsSnapshot is a point-in-time view of congestion controller metrics.
type CCMetricsSnapshot struct {
	Acquired    int64
	Dropped     int64
	LimitUpward int64
	LimitDown   int64
	CurrentLimit int64
	Inflight    int64
	AvgRTTMs    float64
	DropRate    float64
	Utilization float64
}

// Metrics returns a snapshot.
func (c *CongestionController) Metrics() CCMetricsSnapshot {
	acq := atomic.LoadInt64(&c.metrics.acquired)
	drp := atomic.LoadInt64(&c.metrics.dropped)
	rttNs := atomic.LoadInt64(&c.metrics.totalRTTNs)
	lim := atomic.LoadInt64(&c.limit)
	inf := atomic.LoadInt64(&c.inflight)

	var avgRTT, dropRate, util float64
	if acq > 0 {
		avgRTT = float64(rttNs) / float64(acq) / 1e6
	}
	if acq+drp > 0 {
		dropRate = float64(drp) / float64(acq+drp)
	}
	if lim > 0 {
		util = float64(inf) / float64(lim)
	}
	return CCMetricsSnapshot{
		Acquired:     acq,
		Dropped:      drp,
		LimitUpward:  atomic.LoadInt64(&c.metrics.limitUp),
		LimitDown:    atomic.LoadInt64(&c.metrics.limitDown),
		CurrentLimit: lim,
		Inflight:     inf,
		AvgRTTMs:     avgRTT,
		DropRate:     dropRate,
		Utilization:  util,
	}
}

// ErrCongestionLimitReached is returned when AcquireOrDrop finds no capacity.
var ErrCongestionLimitReached = fmt.Errorf("flowguard: concurrency limit reached")
