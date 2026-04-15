package ratelimit

import base "github.com/driftappdev/libpackage/ratelimit/limiter"

type Strategy = base.Strategy

const (
	StrategyFixedWindow = base.StrategyFixedWindow
)

type Key = base.Key
type Policy = base.Policy
type Result = base.Result
type Decision = base.Decision
type DecisionHook = base.DecisionHook
type Store = base.Store
type Options = base.Options
type Limiter = base.Limiter

var (
	ErrLimited = base.ErrLimited
	ErrNoStore = base.ErrNoStore
)

func New(opts Options) *Limiter {
	return base.New(opts)
}
