// Package gotimeout provides production-grade timeout primitives:
// call timeouts, deadline propagation, adaptive timeouts, and fallback execution.
package gotimeout

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ---- Sentinel errors --------------------------------------------------------

// ErrTimedOut is returned when an operation exceeds its deadline.
var ErrTimedOut = errors.New("gotimeout: operation timed out")

// ErrFallbackFailed is returned when both the primary and fallback fail.
var ErrFallbackFailed = errors.New("gotimeout: primary and fallback both failed")

// ---- Do / DoT ---------------------------------------------------------------

// Do runs fn within the given timeout. If fn does not complete in time,
// it returns ErrTimedOut. The context passed to fn is automatically cancelled
// when the timeout fires or fn returns.
func Do(ctx context.Context, timeout time.Duration, fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch := make(chan error, 1)
	go func() {
		ch <- fn(ctx)
	}()

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w after %s", ErrTimedOut, timeout)
		}
		return ctx.Err()
	}
}

// DoT runs fn and returns (T, error) within the given timeout.
func DoT[T any](ctx context.Context, timeout time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch := make(chan result, 1)
	go func() {
		v, err := fn(ctx)
		ch <- result{v, err}
	}()

	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return zero, fmt.Errorf("%w after %s", ErrTimedOut, timeout)
		}
		return zero, ctx.Err()
	}
}

// ---- WithFallback -----------------------------------------------------------

// WithFallback runs primary; if it times out or fails, it runs fallback.
// Both fn and fallback receive the same parent context.
func WithFallback[T any](
	ctx context.Context,
	timeout time.Duration,
	primary func(ctx context.Context) (T, error),
	fallback func(ctx context.Context) (T, error),
) (T, error) {
	v, err := DoT(ctx, timeout, primary)
	if err == nil {
		return v, nil
	}
	vf, ef := DoT(ctx, timeout, fallback)
	if ef == nil {
		return vf, nil
	}
	var zero T
	return zero, fmt.Errorf("%w: primary=%v fallback=%v", ErrFallbackFailed, err, ef)
}

// ---- Adaptive timeout -------------------------------------------------------

// AdaptiveTimer tracks latency history and recommends a timeout that covers
// the configured percentile of observed calls.
type AdaptiveTimer struct {
	mu         sync.Mutex
	samples    []time.Duration
	maxSamples int
	percentile float64 // 0.0–1.0
	minTimeout time.Duration
	maxTimeout time.Duration
	current    atomic.Int64 // nanoseconds
}

// AdaptiveOptions configures an AdaptiveTimer.
type AdaptiveOptions struct {
	// MaxSamples is the rolling window size. Default 1000.
	MaxSamples int
	// Percentile (0–1) used for timeout recommendation. Default 0.95.
	Percentile float64
	// MinTimeout floors the recommended timeout. Default 50ms.
	MinTimeout time.Duration
	// MaxTimeout caps the recommended timeout. Default 30s.
	MaxTimeout time.Duration
	// Initial sets the timeout before enough samples are collected. Default 2s.
	Initial time.Duration
}

// NewAdaptiveTimer creates an AdaptiveTimer with the given options.
func NewAdaptiveTimer(opts AdaptiveOptions) *AdaptiveTimer {
	if opts.MaxSamples == 0 {
		opts.MaxSamples = 1000
	}
	if opts.Percentile == 0 {
		opts.Percentile = 0.95
	}
	if opts.MinTimeout == 0 {
		opts.MinTimeout = 50 * time.Millisecond
	}
	if opts.MaxTimeout == 0 {
		opts.MaxTimeout = 30 * time.Second
	}
	initial := opts.Initial
	if initial == 0 {
		initial = 2 * time.Second
	}
	at := &AdaptiveTimer{
		maxSamples: opts.MaxSamples,
		percentile: opts.Percentile,
		minTimeout: opts.MinTimeout,
		maxTimeout: opts.MaxTimeout,
	}
	at.current.Store(int64(initial))
	return at
}

// Record adds an observed call duration to the sample window.
func (at *AdaptiveTimer) Record(d time.Duration) {
	at.mu.Lock()
	defer at.mu.Unlock()

	at.samples = append(at.samples, d)
	if len(at.samples) > at.maxSamples {
		at.samples = at.samples[len(at.samples)-at.maxSamples:]
	}

	// Recompute recommended timeout.
	sorted := make([]time.Duration, len(at.samples))
	copy(sorted, at.samples)
	sortDurations(sorted)

	idx := int(float64(len(sorted)-1) * at.percentile)
	recommended := sorted[idx]

	if recommended < at.minTimeout {
		recommended = at.minTimeout
	}
	if recommended > at.maxTimeout {
		recommended = at.maxTimeout
	}
	at.current.Store(int64(recommended))
}

// Timeout returns the currently recommended timeout.
func (at *AdaptiveTimer) Timeout() time.Duration {
	return time.Duration(at.current.Load())
}

// Do runs fn using the adaptive timeout, records the observed latency,
// and updates the recommendation for future calls.
func (at *AdaptiveTimer) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	start := time.Now()
	err := Do(ctx, at.Timeout(), fn)
	at.Record(time.Since(start))
	return err
}

// Simple insertion sort (small n, no import of sort package needed).
func sortDurations(s []time.Duration) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// ---- Deadline guard ---------------------------------------------------------

// Guard wraps ctx and panics if fn has not returned within grace after the
// context deadline passes. This is useful in tests / dev mode to surface
// goroutine leaks.
func Guard(ctx context.Context, grace time.Duration, fn func()) {
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()

	var deadline time.Time
	var ok bool
	deadline, ok = ctx.Deadline()
	if !ok {
		<-done
		return
	}

	timer := time.NewTimer(time.Until(deadline) + grace)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		panic(fmt.Sprintf("gotimeout: goroutine leaked past deadline+grace (%s)", grace))
	}
}

// ---- Deadline reporter ------------------------------------------------------

// Remaining returns how much time is left on ctx's deadline.
// If ctx has no deadline it returns (0, false).
func Remaining(ctx context.Context) (time.Duration, bool) {
	dl, ok := ctx.Deadline()
	if !ok {
		return 0, false
	}
	rem := time.Until(dl)
	if rem < 0 {
		rem = 0
	}
	return rem, true
}

// MustHaveAtLeast returns an error if the context has less than min remaining.
func MustHaveAtLeast(ctx context.Context, min time.Duration) error {
	rem, ok := Remaining(ctx)
	if !ok {
		return nil // no deadline set; caller is responsible
	}
	if rem < min {
		return fmt.Errorf("%w: only %s remaining, need at least %s", ErrTimedOut, rem, min)
	}
	return nil
}

// ---- Channel helpers --------------------------------------------------------

// SendWithTimeout sends v to ch, returning ErrTimedOut if the send blocks
// longer than timeout.
func SendWithTimeout[T any](ch chan<- T, v T, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ch <- v:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w on channel send after %s", ErrTimedOut, timeout)
	}
}

// RecvWithTimeout reads from ch, returning (zero, ErrTimedOut) on timeout.
func RecvWithTimeout[T any](ch <-chan T, timeout time.Duration) (T, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case v := <-ch:
		return v, nil
	case <-timer.C:
		var zero T
		return zero, fmt.Errorf("%w on channel recv after %s", ErrTimedOut, timeout)
	}
}
