package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// DeadlinePolicy controls what happens when the deadline is exceeded.
type DeadlinePolicy int

const (
	// DeadlinePolicyCancel cancels the context on deadline (default).
	DeadlinePolicyCancel DeadlinePolicy = iota
	// DeadlinePolicyWarn logs/callbacks but does not cancel.
	DeadlinePolicyWarn
	// DeadlinePolicySLA treats the deadline as an SLA breach alarm only.
	DeadlinePolicySLA
)

// DeadlineConfig configures a DeadlineEnforcer.
type DeadlineConfig struct {
	// Name identifies the enforcer in logs/metrics.
	Name string
	// Hard is the maximum allowed duration; context is cancelled at this point.
	Hard time.Duration
	// Soft is an optional warning threshold before Hard fires.
	Soft time.Duration
	// Policy overrides cancellation behavior.
	Policy DeadlinePolicy
	// OnSoftBreach is called when the soft deadline is exceeded.
	OnSoftBreach func(name string, elapsed time.Duration, budget time.Duration)
	// OnHardBreach is called when the hard deadline fires.
	OnHardBreach func(name string, elapsed time.Duration)
	// OnComplete is called on successful completion with elapsed time.
	OnComplete func(name string, elapsed time.Duration, remaining time.Duration)
}

// DeadlineEnforcer wraps operations with hard (cancelling) and soft (warning)
// deadlines, SLA breach tracking, and per-call budget propagation.
type DeadlineEnforcer struct {
	cfg DeadlineConfig

	// metrics
	calls      atomic.Int64
	hardBreaches atomic.Int64
	softBreaches atomic.Int64
	successes  atomic.Int64
}

// NewDeadlineEnforcer creates a new DeadlineEnforcer.
func NewDeadlineEnforcer(cfg DeadlineConfig) *DeadlineEnforcer {
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	if cfg.Hard <= 0 {
		panic("resilience: DeadlineEnforcer Hard must be > 0")
	}
	return &DeadlineEnforcer{cfg: cfg}
}

// Do executes fn within the configured deadline budget.
// The context passed to fn has the hard deadline applied.
// Returns an error if the hard deadline is exceeded or fn returns an error.
func (d *DeadlineEnforcer) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	d.calls.Add(1)
	start := time.Now()

	// Apply hard deadline
	deadline := start.Add(d.cfg.Hard)
	innerCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	// Soft deadline watcher
	var softOnce sync.Once
	var softTimer *time.Timer
	if d.cfg.Soft > 0 && d.cfg.Soft < d.cfg.Hard && d.cfg.OnSoftBreach != nil {
		softTimer = time.AfterFunc(d.cfg.Soft, func() {
			softOnce.Do(func() {
				elapsed := time.Since(start)
				d.softBreaches.Add(1)
				d.cfg.OnSoftBreach(d.cfg.Name, elapsed, d.cfg.Hard-elapsed)
			})
		})
		defer softTimer.Stop()
	}

	// Execute
	errCh := make(chan error, 1)
	go func() {
		errCh <- fn(innerCtx)
	}()

	select {
	case err := <-errCh:
		elapsed := time.Since(start)
		if err != nil {
			return err
		}
		d.successes.Add(1)
		if d.cfg.OnComplete != nil {
			remaining := d.cfg.Hard - elapsed
			if remaining < 0 {
				remaining = 0
			}
			d.cfg.OnComplete(d.cfg.Name, elapsed, remaining)
		}
		return nil

	case <-innerCtx.Done():
		elapsed := time.Since(start)
		d.hardBreaches.Add(1)
		if d.cfg.OnHardBreach != nil {
			d.cfg.OnHardBreach(d.cfg.Name, elapsed)
		}
		if d.cfg.Policy == DeadlinePolicyWarn || d.cfg.Policy == DeadlinePolicySLA {
			// Wait for fn to finish anyway (soft enforcement)
			select {
			case err := <-errCh:
				return err
			case <-ctx.Done():
				return fmt.Errorf("deadline[%s]: outer context cancelled: %w", d.cfg.Name, ctx.Err())
			}
		}
		return fmt.Errorf("deadline[%s]: hard deadline exceeded after %s: %w",
			d.cfg.Name, elapsed, innerCtx.Err())
	}
}

// WithBudget derives a child context with a proportional budget.
// fraction should be in (0,1]. E.g., 0.8 gives 80% of the remaining deadline.
func (d *DeadlineEnforcer) WithBudget(ctx context.Context, fraction float64) (context.Context, context.CancelFunc) {
	if fraction <= 0 || fraction > 1 {
		fraction = 1
	}
	budget := time.Duration(float64(d.cfg.Hard) * fraction)
	return context.WithTimeout(ctx, budget)
}

// Stats returns a snapshot.
func (d *DeadlineEnforcer) Stats() DeadlineStats {
	return DeadlineStats{
		Name:         d.cfg.Name,
		Calls:        d.calls.Load(),
		Successes:    d.successes.Load(),
		HardBreaches: d.hardBreaches.Load(),
		SoftBreaches: d.softBreaches.Load(),
	}
}

// DeadlineStats is a point-in-time snapshot.
type DeadlineStats struct {
	Name         string
	Calls        int64
	Successes    int64
	HardBreaches int64
	SoftBreaches int64
}

// ─────────────────────────────────────────────────────────────────────────────
// PropagatingDeadline: propagate a fraction of remaining budget downstream
// ─────────────────────────────────────────────────────────────────────────────

// RemainingBudget returns how much time remains on a context deadline.
// Returns (remaining, true) or (0, false) if no deadline is set.
func RemainingBudget(ctx context.Context) (time.Duration, bool) {
	dl, ok := ctx.Deadline()
	if !ok {
		return 0, false
	}
	rem := time.Until(dl)
	if rem < 0 {
		return 0, true
	}
	return rem, true
}

// WithFractionOfBudget creates a child context with a fraction of the
// remaining parent budget. Useful for cascading deadlines across RPC calls.
func WithFractionOfBudget(ctx context.Context, fraction float64) (context.Context, context.CancelFunc, error) {
	rem, ok := RemainingBudget(ctx)
	if !ok {
		return ctx, func() {}, fmt.Errorf("deadline: parent context has no deadline")
	}
	if rem <= 0 {
		ctx, c := context.WithCancel(ctx)
		c()
		return ctx, c, fmt.Errorf("deadline: parent budget already exhausted")
	}
	child := time.Duration(float64(rem) * fraction)
	childCtx, cancel := context.WithTimeout(ctx, child)
	return childCtx, cancel, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// DeadlineGroup: run multiple fns within a shared deadline
// ─────────────────────────────────────────────────────────────────────────────

// DeadlineGroup runs a set of functions concurrently, all sharing a single
// hard deadline. If any function exceeds the deadline or returns an error,
// the group's context is cancelled.
type DeadlineGroup struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	errs   []error
}

// NewDeadlineGroup creates a group with a hard deadline of duration d.
func NewDeadlineGroup(ctx context.Context, d time.Duration) *DeadlineGroup {
	dctx, cancel := context.WithTimeout(ctx, d)
	return &DeadlineGroup{ctx: dctx, cancel: cancel}
}

// Go runs fn in a new goroutine within the group.
func (g *DeadlineGroup) Go(fn func(ctx context.Context) error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := fn(g.ctx); err != nil {
			g.mu.Lock()
			g.errs = append(g.errs, err)
			g.mu.Unlock()
			g.cancel()
		}
	}()
}

// Wait blocks until all goroutines finish and returns all errors.
func (g *DeadlineGroup) Wait() []error {
	g.wg.Wait()
	g.cancel()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ctx.Err() != nil && len(g.errs) == 0 {
		g.errs = append(g.errs, fmt.Errorf("deadline group: %w", g.ctx.Err()))
	}
	return g.errs
}
