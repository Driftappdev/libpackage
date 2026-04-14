package trace

import "context"

type contextKey string

const traceKey contextKey = "midul.trace.id"

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey, traceID)
}
func TraceIDFromContext(ctx context.Context) string { v, _ := ctx.Value(traceKey).(string); return v }
