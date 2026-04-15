package tracing

import "context"

// Span represents a minimal trace span abstraction.
type Span interface {
	End()
	AddField(key string, value any)
}

// Tracer starts spans from context.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

type noopSpan struct{}

func (noopSpan) End()                     {}
func (noopSpan) AddField(_ string, _ any) {}

// NoopTracer is used when tracing is disabled.
type NoopTracer struct{}

func (NoopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, noopSpan{}
}
