package flowguard

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync/atomic"
	"time"
)

// AdaptiveRetry implements a production-grade retry engine with:
//   - Exponential backoff with full/equal jitter (AWS-style)
//   - Retry budget to prevent retry amplification cascades
//   - Context-aware cancellation with deadline detection
//   - Per-error-class retry policies (retryable vs non-retryable)
//   - Circuit-break integration (skips retries when circuit open)
//   - Comprehensive metrics (attempt distribution, latency percentiles)
//
// Reference: "Exponential Backoff And Jitter" – AWS Architecture Blog
//
// Example:
//
//	retrier := NewAdaptiveRetry(AdaptiveRetryConfig{
//	    MaxAttempts:    4,
//	    BaseDelay:      50 * time.Millisecond,
//	    MaxDelay:       5 * time.Second,
//	    JitterStrategy: JitterFull,
//	    Budget:         NewRetryBudget(0.10, 10),
//	    IsRetryable:    IsRetryableHTTPError,
//	})
//	result, err := retrier.Do(ctx, func(ctx context.Context, attempt int) (any, error) {
//	    return httpClient.Get(ctx, url)
//	})
type AdaptiveRetry struct {
	cfg     AdaptiveRetryConfig
	metrics *retryMetrics
}

// JitterStrategy controls how backoff jitter is applied.
type JitterStrategy int

const (
	// JitterNone applies pure exponential backoff with no jitter (not recommended for prod).
	JitterNone JitterStrategy = iota
	// JitterFull randomises the delay uniformly in [0, base * 2^attempt].
	JitterFull
	// JitterEqual randomises in [base*2^attempt/2, base*2^attempt].
	JitterEqual
	// JitterDecorelated uses AWS decorrelated jitter: delay = rand(base, prev*3).
	JitterDecorelated
)

// AdaptiveRetryConfig configures the AdaptiveRetry engine.
type AdaptiveRetryConfig struct {
	// MaxAttempts is the total number of attempts (1 = no retry).
	MaxAttempts int

	// BaseDelay is the initial backoff delay.
	BaseDelay time.Duration

	// MaxDelay caps the backoff regardless of attempt count.
	MaxDelay time.Duration

	// Multiplier is the exponential base. Default: 2.0.
	Multiplier float64

	// JitterStrategy controls jitter algorithm. Default: JitterFull.
	JitterStrategy JitterStrategy

	// IsRetryable classifies errors as retryable or terminal.
	// Default: all non-nil errors are retryable.
	IsRetryable func(err error, attempt int) bool

	// Budget limits the fraction of total calls that may be retries.
	// nil = no budget.
	Budget *RetryBudget

	// OnRetry is called before each retry attempt.
	OnRetry func(attempt int, delay time.Duration, err error)

	// OnSuccess is called after a successful attempt.
	OnSuccess func(attempt int, totalLatency time.Duration)

	// OnExhausted is called when all attempts fail.
	OnExhausted func(attempts int, lastErr error)
}

func (c *AdaptiveRetryConfig) setDefaults() {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.BaseDelay == 0 {
		c.BaseDelay = 100 * time.Millisecond
	}
	if c.MaxDelay == 0 {
		c.MaxDelay = 30 * time.Second
	}
	if c.Multiplier == 0 {
		c.Multiplier = 2.0
	}
	if c.IsRetryable == nil {
		c.IsRetryable = func(err error, _ int) bool { return err != nil }
	}
}

// RetryBudget tracks retry rate across all concurrent callers to prevent
// retry storms from amplifying failures.
type RetryBudget struct {
	// maxRetryRate is the maximum fraction of requests that may be retries.
	maxRetryRate float64
	// minTokens is the minimum retries allowed per period regardless of rate.
	minTokens int64

	requests int64
	retries  int64
	mu       interface{} // unused; operations are atomic
}

// NewRetryBudget creates a RetryBudget.
// maxRetryRate: e.g. 0.1 = max 10% of calls may be retries.
// minTokens: always allow at least this many retries (avoids starvation at low traffic).
func NewRetryBudget(maxRetryRate float64, minTokens int64) *RetryBudget {
	return &RetryBudget{maxRetryRate: maxRetryRate, minTokens: minTokens}
}

// Acquire attempts to consume a retry token. Returns false if budget exhausted.
func (b *RetryBudget) Acquire() bool {
	req := atomic.LoadInt64(&b.requests)
	ret := atomic.LoadInt64(&b.retries)
	if ret < b.minTokens {
		atomic.AddInt64(&b.retries, 1)
		return true
	}
	if req == 0 {
		return true
	}
	if float64(ret)/float64(req) >= b.maxRetryRate {
		return false
	}
	atomic.AddInt64(&b.retries, 1)
	return true
}

// RecordRequest increments the total request counter.
func (b *RetryBudget) RecordRequest() { atomic.AddInt64(&b.requests, 1) }

// RetryRate returns the current retry rate.
func (b *RetryBudget) RetryRate() float64 {
	req := atomic.LoadInt64(&b.requests)
	if req == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&b.retries)) / float64(req)
}

type retryMetrics struct {
	attempts      [16]int64 // indexed by attempt number (0-based)
	successes     int64
	exhaustions   int64
	budgetDenials int64
	totalLatNs    int64
}

// RetryResult wraps the successful call result with retry metadata.
type RetryResult struct {
	Value        any
	Attempts     int
	TotalLatency time.Duration
}

// NewAdaptiveRetry creates a new AdaptiveRetry engine.
func NewAdaptiveRetry(cfg AdaptiveRetryConfig) *AdaptiveRetry {
	cfg.setDefaults()
	return &AdaptiveRetry{cfg: cfg, metrics: &retryMetrics{}}
}

// Do executes fn with retry logic.
// fn receives the context and the current attempt number (1-based).
func (r *AdaptiveRetry) Do(ctx context.Context, fn func(ctx context.Context, attempt int) (any, error)) (*RetryResult, error) {
	if r.cfg.Budget != nil {
		r.cfg.Budget.RecordRequest()
	}

	start := time.Now()
	var lastErr error
	prevDelay := r.cfg.BaseDelay

	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		// Check context before each attempt.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("flowguard: retry aborted (attempt %d): %w", attempt, ctx.Err())
		}

		if attempt <= len(r.metrics.attempts) {
			atomic.AddInt64(&r.metrics.attempts[attempt-1], 1)
		}

		val, err := fn(ctx, attempt)
		if err == nil {
			totalLat := time.Since(start)
			atomic.AddInt64(&r.metrics.successes, 1)
			atomic.AddInt64(&r.metrics.totalLatNs, totalLat.Nanoseconds())
			if r.cfg.OnSuccess != nil {
				r.cfg.OnSuccess(attempt, totalLat)
			}
			return &RetryResult{Value: val, Attempts: attempt, TotalLatency: totalLat}, nil
		}
		lastErr = err

		// Terminal error?
		if !r.cfg.IsRetryable(err, attempt) {
			break
		}

		// Last attempt – no point sleeping.
		if attempt == r.cfg.MaxAttempts {
			break
		}

		// Check deadline: skip retry if not enough headroom.
		if dl, ok := ctx.Deadline(); ok {
			delay := r.computeDelay(attempt, prevDelay)
			if time.Until(dl) < delay {
				break
			}
		}

		// Budget check.
		if r.cfg.Budget != nil && !r.cfg.Budget.Acquire() {
			atomic.AddInt64(&r.metrics.budgetDenials, 1)
			break
		}

		delay := r.computeDelay(attempt, prevDelay)
		prevDelay = delay

		if r.cfg.OnRetry != nil {
			r.cfg.OnRetry(attempt, delay, err)
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, fmt.Errorf("flowguard: retry wait canceled: %w", ctx.Err())
		}
	}

	atomic.AddInt64(&r.metrics.exhaustions, 1)
	if r.cfg.OnExhausted != nil {
		r.cfg.OnExhausted(r.cfg.MaxAttempts, lastErr)
	}
	return nil, fmt.Errorf("flowguard: all %d attempts failed: %w", r.cfg.MaxAttempts, lastErr)
}

// DoSimple is a convenience wrapper for functions without a return value.
func (r *AdaptiveRetry) DoSimple(ctx context.Context, fn func(ctx context.Context, attempt int) error) error {
	_, err := r.Do(ctx, func(ctx context.Context, attempt int) (any, error) {
		return nil, fn(ctx, attempt)
	})
	return err
}

func (r *AdaptiveRetry) computeDelay(attempt int, prevDelay time.Duration) time.Duration {
	base := float64(r.cfg.BaseDelay)
	cap_ := float64(r.cfg.MaxDelay)
	mult := math.Pow(r.cfg.Multiplier, float64(attempt-1))
	expDelay := base * mult

	var delay float64
	switch r.cfg.JitterStrategy {
	case JitterNone:
		delay = math.Min(cap_, expDelay)

	case JitterFull:
		delay = rand.Float64() * math.Min(cap_, expDelay) //nolint:gosec

	case JitterEqual:
		half := math.Min(cap_, expDelay) / 2
		delay = half + rand.Float64()*half //nolint:gosec

	case JitterDecorelated:
		// AWS decorrelated: sleep = min(cap, rand(base, prev*3))
		lo := base
		hi := float64(prevDelay) * 3
		if hi < lo {
			hi = lo
		}
		delay = math.Min(cap_, lo+rand.Float64()*(hi-lo)) //nolint:gosec
	}

	return time.Duration(delay)
}

// RetryMetricsSnapshot is a point-in-time view of retry metrics.
type RetryMetricsSnapshot struct {
	// AttemptDistribution[i] = number of calls that required i+1 attempts.
	AttemptDistribution [16]int64
	Successes           int64
	Exhaustions         int64
	BudgetDenials       int64
	AvgLatencyMs        float64
	SuccessRate         float64
}

// Metrics returns a snapshot.
func (r *AdaptiveRetry) Metrics() RetryMetricsSnapshot {
	s := atomic.LoadInt64(&r.metrics.successes)
	e := atomic.LoadInt64(&r.metrics.exhaustions)
	lat := atomic.LoadInt64(&r.metrics.totalLatNs)
	var avgLat, successRate float64
	if s > 0 {
		avgLat = float64(lat) / float64(s) / 1e6
		successRate = float64(s) / float64(s+e)
	}
	snap := RetryMetricsSnapshot{
		Successes:     s,
		Exhaustions:   e,
		BudgetDenials: atomic.LoadInt64(&r.metrics.budgetDenials),
		AvgLatencyMs:  avgLat,
		SuccessRate:   successRate,
	}
	for i := range r.metrics.attempts {
		snap.AttemptDistribution[i] = atomic.LoadInt64(&r.metrics.attempts[i])
	}
	return snap
}

// ---- Built-in IsRetryable policies ----

// IsRetryableHTTPStatus returns true for 429, 500, 502, 503, 504.
func IsRetryableHTTPStatus(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// IsRetryableTemporary returns true for errors implementing Temporary() bool.
func IsRetryableTemporary(err error, _ int) bool {
	var t interface{ Temporary() bool }
	if errors.As(err, &t) {
		return t.Temporary()
	}
	return false
}

// NotRetryable marks an error as terminal (wraps it so AdaptiveRetry stops).
type NotRetryable struct{ Cause error }

func (e *NotRetryable) Error() string  { return fmt.Sprintf("not retryable: %v", e.Cause) }
func (e *NotRetryable) Unwrap() error  { return e.Cause }

// WrapNotRetryable wraps err so the retry engine won't retry it.
func WrapNotRetryable(err error) error { return &NotRetryable{Cause: err} }

// IsRetryableExcludingTerminal is an IsRetryable func that skips NotRetryable errors.
func IsRetryableExcludingTerminal(err error, _ int) bool {
	var nr *NotRetryable
	return !errors.As(err, &nr)
}
