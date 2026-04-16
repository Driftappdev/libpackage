package tx

import (
    "context"
    "time"
)

type Propagation int

const (
    PropagationRequired Propagation = iota
    PropagationRequiresNew
    PropagationSupports
    PropagationMandatory
    PropagationNever
)

type Options struct {
    Name            string
    Timeout         time.Duration
    Isolation       IsolationLevel
    ReadOnly        bool
    Propagation     Propagation
    RollbackOnPanic bool
}

func DefaultOptions() Options {
    return Options{Isolation: IsolationDefault, Propagation: PropagationRequired, RollbackOnPanic: true}
}

type Option func(*Options)

func WithName(name string) Option        { return func(o *Options) { o.Name = name } }
func WithTimeout(d time.Duration) Option { return func(o *Options) { o.Timeout = d } }
func WithIsolation(l IsolationLevel) Option { return func(o *Options) { o.Isolation = l } }
func WithReadOnly(v bool) Option         { return func(o *Options) { o.ReadOnly = v } }
func WithPropagation(p Propagation) Option { return func(o *Options) { o.Propagation = p } }
func WithRollbackOnPanic(v bool) Option  { return func(o *Options) { o.RollbackOnPanic = v } }

func BuildOptions(opts ...Option) Options {
    out := DefaultOptions()
    for _, opt := range opts {
        if opt != nil {
            opt(&out)
        }
    }
    return out
}

func applyTimeout(ctx context.Context, o Options) (context.Context, context.CancelFunc) {
    if o.Timeout <= 0 {
        return ctx, func() {}
    }
    return context.WithTimeout(ctx, o.Timeout)
}
