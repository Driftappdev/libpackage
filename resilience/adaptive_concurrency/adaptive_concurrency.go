package resilience

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// AdaptiveAlgorithm selects the concurrency limit algorithm.
type AdaptiveAlgorithm int

const (
	// AlgoAIMD — Additive Increase / Multiplicative Decrease (TCP Congestion Control)
	AlgoAIMD AdaptiveAlgorithm = iota
	// AlgoGradient — gradient-based (Netflix CONCURRENCY-LIMITS style)
	AlgoGradient
	// AlgoVegas — TCP Vegas latency-based adaptation
	AlgoVegas
)

// AdaptiveConcurrencyConfig configures the adaptive limiter.
type AdaptiveConcurrencyConfig struct {
	Name string
	// Algorithm selects the adaptation method.
	Algorithm AdaptiveAlgorithm
	// InitialLimit is the starting concurrency limit.
	InitialLimit int
	// MinLimit is the floor for the limit.
	MinLimit int
	// MaxLimit is the ceiling for the limit.
	MaxLimit int
	// SampleWindow is how often the algorithm recalculates.
	SampleWindow time.Duration
	// TargetRTT is the "good" RTT baseline (Vegas/Gradient).
	TargetRTT time.Duration
	// BackoffFactor for AIMD decrease (default 0.9).
	BackoffFactor float64
	// OnLimitChange is called when the concurrency limit changes.
	OnLimitChange func(name string, oldLimit, newLimit int)
	// OnOverload is called when latency signals overload.
	OnOverload func(name string, latency time.Duration)
}

// AdaptiveConcurrencyLimiter dynamically adjusts its concurrency limit
// based on observed latency signals. It uses the configured algorithm
// (AIMD, Gradient, or Vegas) to find the optimal limit without prior
// capacity knowledge.
type AdaptiveConcurrencyLimiter struct {
	cfg     AdaptiveConcurrencyConfig
	mu      sync.Mutex
	limit   int
	inflight int
	sem     chan struct{}

	// RTT tracking (per window)
	windowStart  time.Time
	windowRTTs   []float64
	minRTT       float64 // long-running minimum RTT (ms)
	rttEWMA      float64

	// metrics
	acquired  atomic.Int64
	rejected  atomic.Int64
	limitBumps atomic.Int64
	limitDrops atomic.Int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAdaptiveConcurrencyLimiter creates and starts the limiter.
func NewAdaptiveConcurrencyLimiter(cfg AdaptiveConcurrencyConfig) *AdaptiveConcurrencyLimiter {
	if cfg.InitialLimit <= 0 {
		cfg.InitialLimit = 20
	}
	if cfg.MinLimit <= 0 {
		cfg.MinLimit = 1
	}
	if cfg.MaxLimit <= 0 {
		cfg.MaxLimit = 1000
	}
	if cfg.SampleWindow <= 0 {
		cfg.SampleWindow = 1 * time.Second
	}
	if cfg.BackoffFactor <= 0 || cfg.BackoffFactor >= 1 {
		cfg.BackoffFactor = 0.9
	}
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &AdaptiveConcurrencyLimiter{
		cfg:         cfg,
		limit:       cfg.InitialLimit,
		windowStart: time.Now(),
		ctx:         ctx,
		cancel:      cancel,
	}
	l.sem = make(chan struct{}, cfg.MaxLimit)
	for i := 0; i < cfg.InitialLimit; i++ {
		l.sem <- struct{}{}
	}
	l.wg.Add(1)
	go l.adaptLoop()
	return l
}

// Acquire acquires a concurrency slot. Returns a token to release.
func (l *AdaptiveConcurrencyLimiter) Acquire(ctx context.Context) (*AdaptiveToken, error) {
	select {
	case <-l.sem:
		l.mu.Lock()
		l.inflight++
		l.mu.Unlock()
		l.acquired.Add(1)
		return &AdaptiveToken{l: l, start: time.Now()}, nil
	case <-ctx.Done():
		l.rejected.Add(1)
		return nil, fmt.Errorf("adaptive[%s]: context cancelled: %w", l.cfg.Name, ctx.Err())
	}
}

// TryAcquire non-blocking acquire.
func (l *AdaptiveConcurrencyLimiter) TryAcquire() (*AdaptiveToken, bool) {
	select {
	case <-l.sem:
		l.mu.Lock()
		l.inflight++
		l.mu.Unlock()
		l.acquired.Add(1)
		return &AdaptiveToken{l: l, start: time.Now()}, true
	default:
		l.rejected.Add(1)
		return nil, false
	}
}

func (l *AdaptiveConcurrencyLimiter) release(t *AdaptiveToken, success bool) {
	rtt := time.Since(t.start)
	l.mu.Lock()
	l.inflight--
	ms := float64(rtt.Milliseconds())
	l.windowRTTs = append(l.windowRTTs, ms)
	if l.rttEWMA == 0 {
		l.rttEWMA = ms
	} else {
		l.rttEWMA = 0.1*ms + 0.9*l.rttEWMA
	}
	if l.minRTT == 0 || ms < l.minRTT {
		l.minRTT = ms
	}
	// Overload signal: immediately decrease on error
	if !success {
		newLimit := int(float64(l.limit) * l.cfg.BackoffFactor)
		l.applyLimit(newLimit)
	}
	l.mu.Unlock()

	l.sem <- struct{}{} // return token
}

func (l *AdaptiveConcurrencyLimiter) adaptLoop() {
	defer l.wg.Done()
	ticker := time.NewTicker(l.cfg.SampleWindow)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.adapt()
		case <-l.ctx.Done():
			return
		}
	}
}

func (l *AdaptiveConcurrencyLimiter) adapt() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.windowRTTs) == 0 {
		return
	}

	var sum float64
	for _, r := range l.windowRTTs {
		sum += r
	}
	avgRTT := sum / float64(len(l.windowRTTs))
	l.windowRTTs = l.windowRTTs[:0]

	switch l.cfg.Algorithm {
	case AlgoAIMD:
		l.aimd(avgRTT)
	case AlgoGradient:
		l.gradient(avgRTT)
	case AlgoVegas:
		l.vegas(avgRTT)
	}
}

func (l *AdaptiveConcurrencyLimiter) aimd(avgRTT float64) {
	target := float64(l.cfg.TargetRTT.Milliseconds())
	if target <= 0 {
		target = l.minRTT * 2
	}
	if avgRTT > target {
		// Multiplicative decrease
		newLimit := int(float64(l.limit) * l.cfg.BackoffFactor)
		if l.cfg.OnOverload != nil {
			go l.cfg.OnOverload(l.cfg.Name, time.Duration(avgRTT)*time.Millisecond)
		}
		l.applyLimit(newLimit)
	} else {
		// Additive increase
		l.applyLimit(l.limit + 1)
	}
}

func (l *AdaptiveConcurrencyLimiter) gradient(avgRTT float64) {
	if l.minRTT == 0 {
		return
	}
	// gradient = minRTT / measuredRTT
	gradient := l.minRTT / avgRTT
	newLimit := int(float64(l.limit)*gradient) + l.inflight
	l.applyLimit(newLimit)
}

func (l *AdaptiveConcurrencyLimiter) vegas(avgRTT float64) {
	if l.minRTT == 0 {
		return
	}
	// Expected throughput vs actual
	expected := float64(l.limit) / l.minRTT
	actual := float64(l.limit) / avgRTT
	diff := expected - actual

	alpha := math.Max(0.15*float64(l.limit), 1)
	beta := math.Max(0.2*float64(l.limit), 1)

	var newLimit int
	if diff < alpha {
		newLimit = l.limit + 1
	} else if diff > beta {
		newLimit = l.limit - 1
	} else {
		newLimit = l.limit
	}
	l.applyLimit(newLimit)
}

// applyLimit clamps and applies a new limit. Must be called with mu held.
func (l *AdaptiveConcurrencyLimiter) applyLimit(newLimit int) {
	if newLimit < l.cfg.MinLimit {
		newLimit = l.cfg.MinLimit
	}
	if newLimit > l.cfg.MaxLimit {
		newLimit = l.cfg.MaxLimit
	}
	if newLimit == l.limit {
		return
	}
	old := l.limit
	diff := newLimit - old
	l.limit = newLimit

	if diff > 0 {
		for i := 0; i < diff; i++ {
			select {
			case l.sem <- struct{}{}:
			default:
			}
		}
		l.limitBumps.Add(1)
	} else {
		for i := 0; i > diff; i-- {
			select {
			case <-l.sem:
			default:
			}
		}
		l.limitDrops.Add(1)
	}

	if l.cfg.OnLimitChange != nil {
		go l.cfg.OnLimitChange(l.cfg.Name, old, newLimit)
	}
}

// CurrentLimit returns the current concurrency limit.
func (l *AdaptiveConcurrencyLimiter) CurrentLimit() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

// Close stops the adaptation goroutine.
func (l *AdaptiveConcurrencyLimiter) Close() {
	l.cancel()
	l.wg.Wait()
}

// Stats returns a snapshot.
func (l *AdaptiveConcurrencyLimiter) Stats() AdaptiveStats {
	l.mu.Lock()
	lim := l.limit
	inf := l.inflight
	minR := l.minRTT
	ewma := l.rttEWMA
	l.mu.Unlock()
	return AdaptiveStats{
		Name:       l.cfg.Name,
		Limit:      lim,
		InFlight:   inf,
		Acquired:   l.acquired.Load(),
		Rejected:   l.rejected.Load(),
		LimitBumps: l.limitBumps.Load(),
		LimitDrops: l.limitDrops.Load(),
		MinRTT:     time.Duration(minR) * time.Millisecond,
		RTTEWMA:    time.Duration(ewma) * time.Millisecond,
	}
}

// AdaptiveStats is a point-in-time snapshot.
type AdaptiveStats struct {
	Name       string
	Limit      int
	InFlight   int
	Acquired   int64
	Rejected   int64
	LimitBumps int64
	LimitDrops int64
	MinRTT     time.Duration
	RTTEWMA    time.Duration
}

// AdaptiveToken is an acquired slot. Must call Release exactly once.
type AdaptiveToken struct {
	l     *AdaptiveConcurrencyLimiter
	start time.Time
	once  sync.Once
}

// Release returns the slot. success=false signals an error to the algorithm.
func (t *AdaptiveToken) Release(success bool) {
	t.once.Do(func() {
		t.l.release(t, success)
	})
}
