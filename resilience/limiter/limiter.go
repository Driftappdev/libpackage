// Package resilience provides enterprise-grade concurrency and resilience primitives.
package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// LimiterConfig holds configuration for the rate limiter.
type LimiterConfig struct {
	// MaxConcurrent is the maximum number of concurrent operations allowed.
	MaxConcurrent int64
	// QueueSize is the maximum number of waiters in the queue (0 = unlimited).
	QueueSize int
	// AcquireTimeout is the maximum time to wait for a slot (0 = block forever).
	AcquireTimeout time.Duration
	// MetricsCallback is called on every acquire/release for observability.
	MetricsCallback func(event LimiterEvent)
}

// LimiterEvent describes an event emitted by the limiter.
type LimiterEvent struct {
	Type      string        // "acquired", "released", "rejected", "timeout"
	WaitTime  time.Duration // time spent waiting
	InFlight  int64         // current in-flight count
	Timestamp time.Time
}

// Limiter is a semaphore-based concurrency limiter with observability,
// queuing, and timeout support. Safe for concurrent use.
type Limiter struct {
	cfg     LimiterConfig
	sem     chan struct{}
	inFlight atomic.Int64
	waiting  atomic.Int64
	rejected atomic.Int64
	acquired atomic.Int64
	mu       sync.Mutex
}

// NewLimiter constructs a new Limiter. Panics if MaxConcurrent < 1.
func NewLimiter(cfg LimiterConfig) *Limiter {
	if cfg.MaxConcurrent < 1 {
		panic("resilience: Limiter MaxConcurrent must be >= 1")
	}
	size := cfg.QueueSize
	if size <= 0 {
		size = int(cfg.MaxConcurrent) * 10
	}
	l := &Limiter{
		cfg: cfg,
		sem: make(chan struct{}, cfg.MaxConcurrent),
	}
	// Pre-fill semaphore
	for i := int64(0); i < cfg.MaxConcurrent; i++ {
		l.sem <- struct{}{}
	}
	return l
}

// Acquire attempts to acquire a concurrency slot. It blocks until a slot is
// available, the context is cancelled, or the AcquireTimeout fires.
// Returns a release function that MUST be called exactly once when done.
func (l *Limiter) Acquire(ctx context.Context) (release func(), err error) {
	start := time.Now()

	// Fast path: non-blocking attempt
	select {
	case <-l.sem:
		l.inFlight.Add(1)
		l.acquired.Add(1)
		l.emit("acquired", time.Since(start))
		return l.releaseFunc(), nil
	default:
	}

	// Reject if queue is full
	if l.cfg.QueueSize > 0 && int(l.waiting.Load()) >= l.cfg.QueueSize {
		l.rejected.Add(1)
		l.emit("rejected", 0)
		return nil, fmt.Errorf("resilience: limiter queue full (%d waiting)", l.waiting.Load())
	}

	l.waiting.Add(1)
	defer l.waiting.Add(-1)

	// Build wait context
	waitCtx := ctx
	var cancel context.CancelFunc
	if l.cfg.AcquireTimeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, l.cfg.AcquireTimeout)
		defer cancel()
	}

	select {
	case <-l.sem:
		waited := time.Since(start)
		l.inFlight.Add(1)
		l.acquired.Add(1)
		l.emit("acquired", waited)
		return l.releaseFunc(), nil

	case <-waitCtx.Done():
		elapsed := time.Since(start)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("resilience: limiter context cancelled after %s: %w", elapsed, ctx.Err())
		}
		l.rejected.Add(1)
		l.emit("timeout", elapsed)
		return nil, fmt.Errorf("resilience: limiter acquire timeout after %s", elapsed)
	}
}

// TryAcquire attempts a non-blocking acquire. Returns (release, true) on
// success or (nil, false) if no slot is immediately available.
func (l *Limiter) TryAcquire() (release func(), ok bool) {
	select {
	case <-l.sem:
		l.inFlight.Add(1)
		l.acquired.Add(1)
		l.emit("acquired", 0)
		return l.releaseFunc(), true
	default:
		return nil, false
	}
}

func (l *Limiter) releaseFunc() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.inFlight.Add(-1)
			l.sem <- struct{}{}
			l.emit("released", 0)
		})
	}
}

func (l *Limiter) emit(eventType string, wait time.Duration) {
	if l.cfg.MetricsCallback == nil {
		return
	}
	l.cfg.MetricsCallback(LimiterEvent{
		Type:      eventType,
		WaitTime:  wait,
		InFlight:  l.inFlight.Load(),
		Timestamp: time.Now(),
	})
}

// Stats returns current limiter statistics (snapshot, not atomic across fields).
func (l *Limiter) Stats() LimiterStats {
	return LimiterStats{
		InFlight:  l.inFlight.Load(),
		Waiting:   l.waiting.Load(),
		Acquired:  l.acquired.Load(),
		Rejected:  l.rejected.Load(),
		Capacity:  l.cfg.MaxConcurrent,
		Available: int64(len(l.sem)),
	}
}

// LimiterStats is a snapshot of the limiter state.
type LimiterStats struct {
	InFlight  int64
	Waiting   int64
	Acquired  int64
	Rejected  int64
	Capacity  int64
	Available int64
}

// Resize dynamically adjusts the MaxConcurrent limit. Thread-safe.
// Shrinking is best-effort: existing in-flight work is not cancelled.
func (l *Limiter) Resize(newMax int64) error {
	if newMax < 1 {
		return fmt.Errorf("resilience: Limiter newMax must be >= 1")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	old := l.cfg.MaxConcurrent
	diff := newMax - old
	l.cfg.MaxConcurrent = newMax

	if diff > 0 {
		// Growing: add tokens
		for i := int64(0); i < diff; i++ {
			l.sem <- struct{}{}
		}
	} else {
		// Shrinking: drain tokens (non-blocking, best-effort)
		for i := int64(0); i > diff; i-- {
			select {
			case <-l.sem:
			default:
				// Token in use; will naturally not re-enter after release
			}
		}
	}
	return nil
}

// Do is a convenience wrapper: acquires, calls fn, releases.
func (l *Limiter) Do(ctx context.Context, fn func() error) error {
	release, err := l.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}
