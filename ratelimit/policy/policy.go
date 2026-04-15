package ratelimit

import "time"
import "errors"

type Strategy string

var (
	ErrBadWindow       = errors.New("invalid window")
	ErrBadLimit        = errors.New("invalid limit")
	ErrBadCost         = errors.New("invalid cost")
	ErrBadBurst        = errors.New("invalid burst")
	ErrUnsupportedMode = errors.New("unsupported strategy")
)

const (
	StrategyFixedWindow   Strategy = "fixed_window"
	StrategySlidingWindow Strategy = "sliding_window"
	StrategyTokenBucket   Strategy = "token_bucket"
)

type Policy struct {
	Name     string
	Limit    int64
	Window   time.Duration
	Strategy Strategy

	// Cost represents unit consumption per request.
	Cost int64
	// Burst applies to token-bucket strategy.
	Burst int64
	// RefillRatePerSecond applies to token-bucket strategy.
	RefillRatePerSecond float64
	// ShadowMode evaluates limits but never blocks traffic.
	ShadowMode bool
}

func DefaultPolicy() Policy {
	return Policy{
		Name:                "default",
		Limit:               100,
		Window:              time.Minute,
		Strategy:            StrategyFixedWindow,
		Cost:                1,
		Burst:               100,
		RefillRatePerSecond: 100.0 / 60.0,
	}
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
	if p.Burst <= 0 {
		p.Burst = p.Limit
	}
	if p.RefillRatePerSecond <= 0 {
		p.RefillRatePerSecond = float64(p.Limit) / p.Window.Seconds()
	}
	return p
}

func (p Policy) Validate() error {
	p = p.Normalize()
	if p.Window <= 0 {
		return ErrBadWindow
	}
	if p.Limit <= 0 {
		return ErrBadLimit
	}
	if p.Cost <= 0 {
		return ErrBadCost
	}
	if p.Burst <= 0 {
		return ErrBadBurst
	}
	switch p.Strategy {
	case StrategyFixedWindow, StrategySlidingWindow, StrategyTokenBucket:
		return nil
	default:
		return ErrUnsupportedMode
	}
}

func (p Policy) EffectiveLimit() int64 {
	p = p.Normalize()
	return p.Limit
}
