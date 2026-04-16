package outbox

import "time"

type Options struct {
    BatchSize    int
    PollInterval time.Duration
    ReserveFor   time.Duration
    MaxAttempts  int
    StopOnError  bool
}

func DefaultOptions() Options { return Options{BatchSize: 100, PollInterval: time.Second, ReserveFor: 30 * time.Second, MaxAttempts: 10} }

type Option func(*Options)

func WithBatchSize(v int) Option           { return func(o *Options) { if v > 0 { o.BatchSize = v } } }
func WithPollInterval(v time.Duration) Option { return func(o *Options) { if v > 0 { o.PollInterval = v } } }
func WithReserveFor(v time.Duration) Option   { return func(o *Options) { if v > 0 { o.ReserveFor = v } } }
func WithMaxAttempts(v int) Option         { return func(o *Options) { if v > 0 { o.MaxAttempts = v } } }
func WithStopOnError(v bool) Option        { return func(o *Options) { o.StopOnError = v } }

func BuildOptions(opts ...Option) Options {
    o := DefaultOptions()
    for _, opt := range opts { if opt != nil { opt(&o) } }
    return o
}
