package ratelimit

import (
	"context"
	"time"
)

type Store interface {
	Increment(ctx context.Context, key string, window time.Duration, now time.Time) (count int64, resetAt time.Time, err error)
}

type Limiter struct {
	policy Policy
	store  Store
	now    func() time.Time
}

func New(opts Options) *Limiter {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Limiter{policy: opts.Policy, store: opts.Store, now: now}
}

func (l *Limiter) Allow(ctx context.Context, key Key) (Result, error) {
	now := l.now()
	if l.policy.Window <= 0 {
		return Result{}, ErrBadWindow
	}
	count, resetAt, err := l.store.Increment(ctx, key.String(), l.policy.Window, now)
	if err != nil {
		return Result{}, err
	}
	remaining := l.policy.Limit - count
	res := Result{Allowed: count <= l.policy.Limit, Limit: l.policy.Limit, Remaining: max64(0, remaining), ResetAt: resetAt}
	if !res.Allowed {
		return res, ErrLimited
	}
	return res, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
