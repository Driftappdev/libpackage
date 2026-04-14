package tracing

import "context"

type Propagator interface {
	Inject(ctx context.Context, carrier map[string]string)
	Extract(ctx context.Context, carrier map[string]string) context.Context
}

type NoopPropagator struct{}

func (NoopPropagator) Inject(_ context.Context, _ map[string]string)                    {}
func (NoopPropagator) Extract(ctx context.Context, _ map[string]string) context.Context { return ctx }
