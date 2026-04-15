package ratelimit

import "time"
import "context"

type DecisionHook func(Decision)

type Policy struct {
	Name   string
	Limit  int64
	Window time.Duration
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
	return p
}

type Store interface {
	Increment(ctx context.Context, key string, window time.Duration, now time.Time) (count int64, resetAt time.Time, err error)
}

type Result struct {
	Allowed   bool
	Limit     int64
	Remaining int64
	ResetAt   time.Time
}

type Decision struct {
	Key       string
	Result    Result
	Err       error
	Timestamp time.Time
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
