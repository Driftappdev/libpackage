package goratelimit

import (
	"context"
	"errors"
	"time"
)

// Engine is a policy-driven limiter that supports store-based strategies.
type Engine struct {
	policy   Policy
	store    Store
	now      func() time.Time
	failOpen bool
	hook     DecisionHook
}

func New(opts Options) *Engine {
	opts = opts.normalize()
	return &Engine{
		policy:   opts.Policy,
		store:    opts.Store,
		now:      opts.Now,
		failOpen: opts.FailOpen,
		hook:     opts.OnResult,
	}
}

func (l *Engine) UpdatePolicy(p Policy) {
	l.policy = p.Normalize()
}

func (l *Engine) Allow(ctx context.Context, key Key) (Result, error) {
	now := l.now()
	res, err := l.allow(ctx, key, now)
	if l.hook != nil {
		l.hook(Decision{
			Key:       key.String(),
			Result:    res,
			Err:       err,
			Timestamp: now,
		})
	}
	return res, err
}

func (l *Engine) Peek(ctx context.Context, key Key) (Result, error) {
	return l.Allow(ctx, key)
}

func (l *Engine) MustAllow(ctx context.Context, key Key) Result {
	res, err := l.Allow(ctx, key)
	if err != nil && !errors.Is(err, ErrLimited) {
		panic(err)
	}
	return res
}

func (l *Engine) allow(ctx context.Context, key Key, now time.Time) (Result, error) {
	if err := key.Validate(); err != nil {
		return Result{}, err
	}
	if l.store == nil {
		if l.failOpen {
			return Result{Allowed: true, Limit: l.policy.Limit, Remaining: l.policy.Limit}, nil
		}
		return Result{}, ErrNoStore
	}

	p := l.policy.Normalize()
	if evalStore, ok := l.store.(EvalStore); ok {
		resp, err := evalStore.Eval(ctx, StoreRequest{Key: key.String(), Policy: p, Now: now})
		if err != nil {
			if l.failOpen {
				return Result{Allowed: true, Limit: p.Limit, Remaining: p.Limit}, nil
			}
			return Result{}, err
		}
		allowed := resp.Allowed || p.ShadowMode
		res := resultFromStoreResponse(p, now, resp, allowed)
		if !allowed && !p.ShadowMode {
			return res, ErrLimited
		}
		return res, nil
	}

	count := 0
	var resetAt time.Time
	for i := 0; i < p.Cost; i++ {
		c, r, err := l.store.Increment(ctx, key.String(), p.Window, now)
		if err != nil {
			if l.failOpen {
				return Result{Allowed: true, Limit: p.Limit, Remaining: p.Limit}, nil
			}
			return Result{}, err
		}
		count = c
		resetAt = r
	}
	remaining := maxInt(0, p.Limit-count)
	allowed := count <= p.Limit || p.ShadowMode
	retryAfter := time.Duration(0)
	if !allowed {
		retryAfter = maxDuration(0, resetAt.Sub(now))
	}
	res := Result{
		Allowed:    allowed,
		Limit:      p.Limit,
		Remaining:  remaining,
		ResetAfter: maxDuration(0, resetAt.Sub(now)),
		RetryAfter: retryAfter,
	}
	if !allowed && !p.ShadowMode {
		return res, ErrLimited
	}
	return res, nil
}

func resultFromStoreResponse(p Policy, now time.Time, resp StoreResponse, allowed bool) Result {
	limit := p.Limit
	if p.Strategy == StrategyTokenBucket {
		limit = p.Burst
	}
	if resp.Limit > 0 {
		limit = resp.Limit
	}
	retryAfter := time.Duration(0)
	if !allowed && !resp.ResetAt.IsZero() {
		retryAfter = maxDuration(0, resp.ResetAt.Sub(now))
	}
	return Result{
		Allowed:    allowed,
		Limit:      limit,
		Remaining:  maxInt(0, resp.Remaining),
		ResetAfter: maxDuration(0, resp.ResetAt.Sub(now)),
		RetryAfter: retryAfter,
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
