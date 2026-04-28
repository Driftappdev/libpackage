package resilience

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync/atomic"
	"time"
)

// BackoffStrategy determines how delay grows between retries.
type BackoffStrategy int

const (
	BackoffFixed       BackoffStrategy = iota // constant delay
	BackoffLinear                             // delay = base * attempt
	BackoffExponential                        // delay = base * 2^attempt
	BackoffFibonacci                          // delay follows Fibonacci sequence
	BackoffDecorrelated                       // AWS-style decorrelated jitter
)

// RetryConfig configures retry-with-backoff behavior.
type RetryConfig struct {
	// MaxAttempts is the total number of tries (including the first). 0 = infinite.
	MaxAttempts int
	// BaseDelay is the starting delay duration.
	BaseDelay time.Duration
	// MaxDelay caps the delay regardless of strategy.
	MaxDelay time.Duration
	// Strategy selects the backoff algorithm.
	Strategy BackoffStrategy
	// Jitter adds random noise to delay (0.0–1.0 fraction).
	Jitter float64
	// Multiplier scales the delay for exponential/linear (default 2.0).
	Multiplier float64
	// RetryIf determines if the error warrants a retry. nil = retry all errors.
	RetryIf func(attempt int, err error) bool
	// OnRetry is called before each retry attempt.
	OnRetry func(attempt int, delay time.Duration, err error)
	// OnSuccess is called after a successful attempt.
	OnSuccess func(attempt int, totalDuration time.Duration)
	// OnFailure is called after all attempts are exhausted.
	OnFailure func(attempts int, totalDuration time.Duration, lastErr error)
}

// RetryStats tracks retry execution metrics.
type RetryStats struct {
	Attempts      int
	TotalDelay    time.Duration
	TotalDuration time.Duration
	LastErr       error
	Succeeded     bool
}

var (
	// ErrMaxAttemptsExceeded is wrapped in the final error when retries are exhausted.
	ErrMaxAttemptsExceeded = errors.New("max attempts exceeded")
	// ErrRetryNotAllowed is returned when RetryIf returns false.
	ErrRetryNotAllowed = errors.New("retry not allowed for this error")
)

// Retryer is a stateless retry executor. Create once, use many times.
type Retryer struct {
	cfg RetryConfig
	rng *rand.Rand
}

// NewRetryer constructs a Retryer with sane defaults.
func NewRetryer(cfg RetryConfig) *Retryer {
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 100 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 2.0
	}
	if cfg.Jitter < 0 || cfg.Jitter > 1 {
		cfg.Jitter = 0.2
	}
	return &Retryer{
		cfg: cfg,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Do executes fn with retry-backoff semantics.
// fn receives the attempt index (0-based).
// Returns the last error wrapped with retry metadata.
func (r *Retryer) Do(ctx context.Context, fn func(ctx context.Context, attempt int) error) (*RetryStats, error) {
	stats := &RetryStats{}
	start := time.Now()
	var lastDelay time.Duration

	for attempt := 0; ; attempt++ {
		stats.Attempts = attempt + 1

		err := fn(ctx, attempt)
		if err == nil {
			stats.Succeeded = true
			stats.TotalDuration = time.Since(start)
			if r.cfg.OnSuccess != nil {
				r.cfg.OnSuccess(attempt, stats.TotalDuration)
			}
			return stats, nil
		}

		stats.LastErr = err

		// Check if we should retry this error
		if r.cfg.RetryIf != nil && !r.cfg.RetryIf(attempt, err) {
			stats.TotalDuration = time.Since(start)
			if r.cfg.OnFailure != nil {
				r.cfg.OnFailure(attempt+1, stats.TotalDuration, err)
			}
			return stats, fmt.Errorf("%w: %w", ErrRetryNotAllowed, err)
		}

		// Check attempt limit
		if r.cfg.MaxAttempts > 0 && attempt+1 >= r.cfg.MaxAttempts {
			stats.TotalDuration = time.Since(start)
			if r.cfg.OnFailure != nil {
				r.cfg.OnFailure(attempt+1, stats.TotalDuration, err)
			}
			return stats, fmt.Errorf("%w after %d attempts: %w", ErrMaxAttemptsExceeded, attempt+1, err)
		}

		// Compute next delay
		delay := r.computeDelay(attempt, lastDelay)
		lastDelay = delay
		stats.TotalDelay += delay

		if r.cfg.OnRetry != nil {
			r.cfg.OnRetry(attempt+1, delay, err)
		}

		// Wait for delay or context cancellation
		select {
		case <-ctx.Done():
			stats.TotalDuration = time.Since(start)
			return stats, fmt.Errorf("retry aborted after %d attempts: %w", attempt+1, ctx.Err())
		case <-time.After(delay):
		}
	}
}

func (r *Retryer) computeDelay(attempt int, lastDelay time.Duration) time.Duration {
	base := r.cfg.BaseDelay
	max := r.cfg.MaxDelay
	mult := r.cfg.Multiplier

	var delay time.Duration

	switch r.cfg.Strategy {
	case BackoffFixed:
		delay = base

	case BackoffLinear:
		delay = time.Duration(float64(base) * float64(attempt+1) * mult)

	case BackoffExponential:
		exp := math.Pow(mult, float64(attempt))
		delay = time.Duration(float64(base) * exp)

	case BackoffFibonacci:
		a, b := 0, 1
		for i := 0; i < attempt; i++ {
			a, b = b, a+b
		}
		delay = time.Duration(float64(base) * float64(b))

	case BackoffDecorrelated:
		// AWS decorrelated jitter: delay = rand(base, prev*3)
		prev := lastDelay
		if prev < base {
			prev = base
		}
		minV := float64(base)
		maxV := float64(prev) * 3
		delay = time.Duration(minV + r.rng.Float64()*(maxV-minV))
	}

	// Apply jitter
	if r.cfg.Jitter > 0 {
		jitter := time.Duration(float64(delay) * r.cfg.Jitter * r.rng.Float64())
		delay += jitter
	}

	// Cap at max
	if delay > max {
		delay = max
	}
	return delay
}

// ─────────────────────────────────────────────────────────────────────────────
// Convenience constructors
// ─────────────────────────────────────────────────────────────────────────────

// ExponentialRetryer returns a retryer with exponential backoff + jitter.
func ExponentialRetryer(maxAttempts int, base, maxDelay time.Duration) *Retryer {
	return NewRetryer(RetryConfig{
		MaxAttempts: maxAttempts,
		BaseDelay:   base,
		MaxDelay:    maxDelay,
		Strategy:    BackoffExponential,
		Jitter:      0.25,
		Multiplier:  2.0,
	})
}

// LinearRetryer returns a retryer with linear backoff.
func LinearRetryer(maxAttempts int, base, maxDelay time.Duration) *Retryer {
	return NewRetryer(RetryConfig{
		MaxAttempts: maxAttempts,
		BaseDelay:   base,
		MaxDelay:    maxDelay,
		Strategy:    BackoffLinear,
		Jitter:      0.1,
		Multiplier:  1.0,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// RetryCounter: track retry stats across many calls
// ─────────────────────────────────────────────────────────────────────────────

// RetryCounter provides aggregate counters across multiple retry executions.
type RetryCounter struct {
	totalCalls    atomic.Int64
	totalRetries  atomic.Int64
	totalFailures atomic.Int64
	totalSuccesses atomic.Int64
}

func (c *RetryCounter) Record(stats *RetryStats) {
	c.totalCalls.Add(1)
	c.totalRetries.Add(int64(stats.Attempts - 1))
	if stats.Succeeded {
		c.totalSuccesses.Add(1)
	} else {
		c.totalFailures.Add(1)
	}
}

func (c *RetryCounter) Snapshot() (calls, retries, failures, successes int64) {
	return c.totalCalls.Load(), c.totalRetries.Load(),
		c.totalFailures.Load(), c.totalSuccesses.Load()
}
