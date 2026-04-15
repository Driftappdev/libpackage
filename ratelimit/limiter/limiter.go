package ratelimit

import (
	"context"
	"errors"
	"time"
)

var (
	ErrLimited = errors.New("rate limited")
	ErrNoStore = errors.New("store is nil")
)

type Strategy string

const (
	StrategyFixedWindow Strategy = "fixed_window"
)

type Key struct {
	Namespace string
	Identity  string
}

func (k Key) String() string {
	if k.Namespace == "" {
		return k.Identity
	}
	return k.Namespace + ":" + k.Identity
}

type Policy struct {
	Name       string
	Limit      int64
	Window     time.Duration
	Strategy   Strategy
	Cost       int64
	ShadowMode bool
}

func (p Policy) Normalize() Policy {
	if p.Name == "" {
		p.Name = "default"
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Window <= 0 {
		p.Window = time.Minute
	}
	if p.Strategy == "" {
		p.Strategy = StrategyFixedWindow
	}
	if p.Cost <= 0 {
		p.Cost = 1
	}
	return p
}

type Result struct {
	Allowed    bool
	Limit      int64
	Remaining  int64
	Used       int64
	ResetAt    time.Time
	RetryAfter time.Duration
	PolicyName string
	Strategy   Strategy
	ShadowMode bool
}

type Decision struct {
	Key       string
	Result    Result
	Err       error
	Timestamp time.Time
}

type DecisionHook func(Decision)

type Store interface {
	Increment(ctx context.Context, key string, window time.Duration, now time.Time) (count int64, resetAt time.Time, err error)
}

type Options struct {
	Policy   Policy
	Store    Store
	Now      func() time.Time
	FailOpen bool
	OnResult DecisionHook
}

func (o Options) normalize() Options {
	o.Policy = o.Policy.Normalize()
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

type Limiter struct {
	policy   Policy
	store    Store
	now      func() time.Time
	failOpen bool
	hook     DecisionHook
}

func New(opts Options) *Limiter {
	opts = opts.normalize()
	return &Limiter{
		policy:   opts.Policy,
		store:    opts.Store,
		now:      opts.Now,
		failOpen: opts.FailOpen,
		hook:     opts.OnResult,
	}
}

func (l *Limiter) Allow(ctx context.Context, key Key) (Result, error) {
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

func (l *Limiter) allow(ctx context.Context, key Key, now time.Time) (Result, error) {
	if l.store == nil {
		if l.failOpen {
			return Result{Allowed: true, PolicyName: l.policy.Name}, nil
		}
		return Result{}, ErrNoStore
	}

	p := l.policy.Normalize()
	count := int64(0)
	var resetAt time.Time
	for i := int64(0); i < p.Cost; i++ {
		c, r, err := l.store.Increment(ctx, key.String(), p.Window, now)
		if err != nil {
			if l.failOpen {
				return Result{
					Allowed:    true,
					Limit:      p.Limit,
					Remaining:  p.Limit,
					Used:       0,
					ResetAt:    now.Add(p.Window),
					PolicyName: p.Name,
					Strategy:   p.Strategy,
				}, nil
			}
			return Result{}, err
		}
		count = c
		resetAt = r
	}

	remaining := max64(0, p.Limit-count)
	res := Result{
		Allowed:    count <= p.Limit || p.ShadowMode,
		Limit:      p.Limit,
		Remaining:  remaining,
		Used:       count,
		ResetAt:    resetAt,
		PolicyName: p.Name,
		Strategy:   p.Strategy,
		ShadowMode: p.ShadowMode,
	}
	if !res.Allowed && !p.ShadowMode {
		res.RetryAfter = maxDuration(0, resetAt.Sub(now))
		return res, ErrLimited
	}
	return res, nil
}

func (l *Limiter) Peek(ctx context.Context, key Key) (Result, error) {
	return l.Allow(ctx, key)
}

func (l *Limiter) MustAllow(ctx context.Context, key Key) Result {
	res, err := l.Allow(ctx, key)
	if err != nil && !errors.Is(err, ErrLimited) {
		panic(err)
	}
	return res
}

func (l *Limiter) UpdatePolicy(p Policy) {
	l.policy = p.Normalize()
}

func max64(a, b int64) int64 {
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
