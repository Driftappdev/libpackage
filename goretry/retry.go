// Package goretry provides a production-grade retry engine with exponential
// back-off, jitter, per-attempt timeouts, context cancellation, and hooks.
package goretry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// -----------------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------------

// BackoffStrategy selects the delay algorithm between attempts.
type BackoffStrategy int

const (
	BackoffExponential BackoffStrategy = iota // delay doubles each attempt
	BackoffLinear                             // delay grows linearly
	BackoffFixed                              // constant delay
	BackoffFibonacci                          // delay follows Fibonacci sequence
)

// Config holds all retry parameters.
type Config struct {
	// MaxAttempts is the total number of tries (including the first). 0 = unlimited.
	MaxAttempts int

	// InitialDelay is the wait before the second attempt.
	InitialDelay time.Duration

	// MaxDelay caps the calculated delay.
	MaxDelay time.Duration

	// Multiplier scales the delay for Exponential / Linear strategies.
	Multiplier float64

	// Jitter adds randomness ±Jitter*delay to avoid thundering herds.
	Jitter float64 // 0.0–1.0

	// Strategy selects the back-off algorithm.
	Strategy BackoffStrategy

	// RetryIf determines whether an error should trigger a retry.
	// If nil, all errors are retried.
	RetryIf func(err error) bool

	// OnRetry is called before each retry attempt.
	OnRetry func(attempt int, delay time.Duration, err error)

	// PerAttemptTimeout limits the duration of a single attempt.
	// Zero means no per-attempt timeout.
	PerAttemptTimeout time.Duration
}

// DefaultConfig returns a sensible starting configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
		Strategy:     BackoffExponential,
	}
}

// -----------------------------------------------------------------------------
// Attempt metadata
// -----------------------------------------------------------------------------

// AttemptInfo describes a single attempt made by the retry engine.
type AttemptInfo struct {
	Number    int
	StartedAt time.Time
	Duration  time.Duration
	Err       error
}

// Result summarises the overall retry run.
type Result struct {
	Attempts []AttemptInfo
	Total    time.Duration
	Err      error
}

// Succeeded reports whether the operation eventually succeeded.
func (r Result) Succeeded() bool { return r.Err == nil }

// LastErr returns the last error encountered, or nil.
func (r Result) LastErr() error { return r.Err }

// -----------------------------------------------------------------------------
// Core retry logic
// -----------------------------------------------------------------------------

// Do executes fn according to cfg, respecting ctx cancellation.
// It returns a Result detailing every attempt.
func Do(ctx context.Context, cfg Config, fn func(ctx context.Context) error) Result {
	start := time.Now()
	var attempts []AttemptInfo

	for attempt := 1; ; attempt++ {
		if cfg.MaxAttempts > 0 && attempt > cfg.MaxAttempts {
			last := lastErr(attempts)
			return Result{Attempts: attempts, Total: time.Since(start), Err: last}
		}

		aCtx := ctx
		var cancel context.CancelFunc
		if cfg.PerAttemptTimeout > 0 {
			aCtx, cancel = context.WithTimeout(ctx, cfg.PerAttemptTimeout)
		}

		aStart := time.Now()
		err := fn(aCtx)
		dur := time.Since(aStart)

		if cancel != nil {
			cancel()
		}

		info := AttemptInfo{Number: attempt, StartedAt: aStart, Duration: dur, Err: err}
		attempts = append(attempts, info)

		if err == nil {
			return Result{Attempts: attempts, Total: time.Since(start)}
		}

		// Context cancelled / deadline exceeded → don't retry.
		if ctx.Err() != nil {
			return Result{Attempts: attempts, Total: time.Since(start), Err: ctx.Err()}
		}

		// Check if this error is retryable.
		if cfg.RetryIf != nil && !cfg.RetryIf(err) {
			return Result{Attempts: attempts, Total: time.Since(start), Err: err}
		}

		// Calculate back-off delay.
		delay := calcDelay(cfg, attempt)

		if cfg.OnRetry != nil {
			cfg.OnRetry(attempt, delay, err)
		}

		// Wait or respect context cancellation.
		select {
		case <-ctx.Done():
			return Result{Attempts: attempts, Total: time.Since(start), Err: ctx.Err()}
		case <-time.After(delay):
		}
	}
}

// DoSimple is a convenience wrapper returning only the final error.
func DoSimple(ctx context.Context, cfg Config, fn func(ctx context.Context) error) error {
	return Do(ctx, cfg, fn).Err
}

// calcDelay computes the back-off delay for a given attempt (1-based).
func calcDelay(cfg Config, attempt int) time.Duration {
	base := cfg.InitialDelay
	mult := cfg.Multiplier
	if mult == 0 {
		mult = 2
	}

	var delay time.Duration
	switch cfg.Strategy {
	case BackoffLinear:
		delay = time.Duration(float64(base) * float64(attempt) * mult)
	case BackoffFixed:
		delay = base
	case BackoffFibonacci:
		delay = time.Duration(float64(base) * float64(fib(attempt)))
	default: // Exponential
		delay = time.Duration(float64(base) * math.Pow(mult, float64(attempt-1)))
	}

	// Apply jitter.
	if cfg.Jitter > 0 {
		jitterRange := float64(delay) * cfg.Jitter
		delta := (rand.Float64()*2 - 1) * jitterRange // -jitter..+jitter
		delay += time.Duration(delta)
	}

	if delay < 0 {
		delay = 0
	}
	if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	return delay
}

func fib(n int) int {
	a, b := 0, 1
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a
}

func lastErr(attempts []AttemptInfo) error {
	if len(attempts) == 0 {
		return nil
	}
	return attempts[len(attempts)-1].Err
}

// -----------------------------------------------------------------------------
// Helpers & composable predicates
// -----------------------------------------------------------------------------

// IsRetryable returns a RetryIf function that retries on any of the given errors.
func IsRetryable(errs ...error) func(error) bool {
	return func(err error) bool {
		for _, e := range errs {
			if errors.Is(err, e) {
				return true
			}
		}
		return false
	}
}

// IsNotRetryable returns a RetryIf function that skips retry for the given errors.
func IsNotRetryable(errs ...error) func(error) bool {
	return func(err error) bool {
		for _, e := range errs {
			if errors.Is(err, e) {
				return false
			}
		}
		return true
	}
}

// Combine merges multiple RetryIf predicates with AND semantics.
func Combine(fns ...func(error) bool) func(error) bool {
	return func(err error) bool {
		for _, fn := range fns {
			if !fn(err) {
				return false
			}
		}
		return true
	}
}

// -----------------------------------------------------------------------------
// Typed retry for operations that return (T, error)
// -----------------------------------------------------------------------------

// DoT retries a generic function returning (T, error).
func DoT[T any](ctx context.Context, cfg Config, fn func(ctx context.Context) (T, error)) (T, Result) {
	var last T
	result := Do(ctx, cfg, func(ctx context.Context) error {
		v, err := fn(ctx)
		if err == nil {
			last = v
		}
		return err
	})
	return last, result
}

// -----------------------------------------------------------------------------
// Deadline-aware retry
// -----------------------------------------------------------------------------

// ErrMaxAttemptsExceeded is returned when all attempts are exhausted.
var ErrMaxAttemptsExceeded = fmt.Errorf("goretry: max attempts exceeded")

// Builder provides a fluent API for configuring retry.
type Builder struct {
	cfg Config
}

// NewBuilder creates a Builder with default config.
func NewBuilder() *Builder { return &Builder{cfg: DefaultConfig()} }

func (b *Builder) MaxAttempts(n int) *Builder            { b.cfg.MaxAttempts = n; return b }
func (b *Builder) InitialDelay(d time.Duration) *Builder { b.cfg.InitialDelay = d; return b }
func (b *Builder) MaxDelay(d time.Duration) *Builder     { b.cfg.MaxDelay = d; return b }
func (b *Builder) Multiplier(m float64) *Builder         { b.cfg.Multiplier = m; return b }
func (b *Builder) WithJitter(j float64) *Builder         { b.cfg.Jitter = j; return b }
func (b *Builder) Strategy(s BackoffStrategy) *Builder   { b.cfg.Strategy = s; return b }
func (b *Builder) RetryIf(fn func(error) bool) *Builder  { b.cfg.RetryIf = fn; return b }
func (b *Builder) OnRetry(fn func(int, time.Duration, error)) *Builder {
	b.cfg.OnRetry = fn
	return b
}
func (b *Builder) PerAttemptTimeout(d time.Duration) *Builder {
	b.cfg.PerAttemptTimeout = d
	return b
}
func (b *Builder) Do(ctx context.Context, fn func(ctx context.Context) error) Result {
	return Do(ctx, b.cfg, fn)
}
func (b *Builder) Build() Config { return b.cfg }
