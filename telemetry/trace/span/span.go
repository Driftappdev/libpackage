package trace

import "context"

type Span interface {
	End()
	SetAttribute(key string, value any)
}

type spanKey struct{}

func WithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

func FromContext(ctx context.Context) Span {
	v, _ := ctx.Value(spanKey{}).(Span)
	return v
}
