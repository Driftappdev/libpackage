package inbox

import "time"

type Options struct {
    HandlerKeyFunc func(Message) string
    LockTTL        time.Duration
}

func DefaultOptions() Options { return Options{HandlerKeyFunc: func(m Message) string { return m.Topic }, LockTTL: 30 * time.Second} }

type Option func(*Options)

func WithHandlerKeyFunc(fn func(Message) string) Option { return func(o *Options) { if fn != nil { o.HandlerKeyFunc = fn } } }
func WithLockTTL(v time.Duration) Option                { return func(o *Options) { if v > 0 { o.LockTTL = v } } }
func BuildOptions(opts ...Option) Options { o := DefaultOptions(); for _, opt := range opts { if opt != nil { opt(&o) } }; return o }
