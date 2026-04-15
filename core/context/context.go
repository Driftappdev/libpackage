package context

import "context"

type contextKey uint8

const (
	requestIDKey contextKey = iota + 1
	correlationIDKey
	traceIDKey
	spanIDKey
	userIDKey
	actorIDKey
)

type Identity struct {
	UserID  string
	ActorID string
}

type RequestMeta struct {
	RequestID     string
	CorrelationID string
	TraceID       string
	SpanID        string
	UserID        string
	ActorID       string
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestIDKey, requestID)
}

func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

func MustRequestID(ctx context.Context, fallback func() string) string {
	if v := RequestID(ctx); v != "" {
		return v
	}
	if fallback != nil {
		return fallback()
	}
	return ""
}

func HasRequestID(ctx context.Context) bool {
	return RequestID(ctx) != ""
}

func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, correlationIDKey, correlationID)
}

func CorrelationID(ctx context.Context) string {
	v, _ := ctx.Value(correlationIDKey).(string)
	return v
}

func HasCorrelationID(ctx context.Context) bool {
	return CorrelationID(ctx) != ""
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, traceIDKey, traceID)
}

func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

func HasTraceID(ctx context.Context) bool {
	return TraceID(ctx) != ""
}

func WithSpanID(ctx context.Context, spanID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, spanIDKey, spanID)
}

func SpanID(ctx context.Context) string {
	v, _ := ctx.Value(spanIDKey).(string)
	return v
}

func HasSpanID(ctx context.Context) bool {
	return SpanID(ctx) != ""
}

func WithUserID(ctx context.Context, userID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, userIDKey, userID)
}

func UserID(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

func HasUserID(ctx context.Context) bool {
	return UserID(ctx) != ""
}

func WithActorID(ctx context.Context, actorID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, actorIDKey, actorID)
}

func ActorID(ctx context.Context) string {
	v, _ := ctx.Value(actorIDKey).(string)
	return v
}

func HasActorID(ctx context.Context) bool {
	return ActorID(ctx) != ""
}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	ctx = WithUserID(ctx, identity.UserID)
	ctx = WithActorID(ctx, identity.ActorID)
	return ctx
}

func IdentityFromContext(ctx context.Context) Identity {
	return Identity{
		UserID:  UserID(ctx),
		ActorID: ActorID(ctx),
	}
}

func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	ctx = WithRequestID(ctx, meta.RequestID)
	ctx = WithCorrelationID(ctx, meta.CorrelationID)
	ctx = WithTraceID(ctx, meta.TraceID)
	ctx = WithSpanID(ctx, meta.SpanID)
	ctx = WithUserID(ctx, meta.UserID)
	ctx = WithActorID(ctx, meta.ActorID)
	return ctx
}

func RequestMetaFromContext(ctx context.Context) RequestMeta {
	return RequestMeta{
		RequestID:     RequestID(ctx),
		CorrelationID: CorrelationID(ctx),
		TraceID:       TraceID(ctx),
		SpanID:        SpanID(ctx),
		UserID:        UserID(ctx),
		ActorID:       ActorID(ctx),
	}
}

func ToMap(ctx context.Context) map[string]string {
	meta := RequestMetaFromContext(ctx)
	out := map[string]string{}
	if meta.RequestID != "" {
		out["request_id"] = meta.RequestID
	}
	if meta.CorrelationID != "" {
		out["correlation_id"] = meta.CorrelationID
	}
	if meta.TraceID != "" {
		out["trace_id"] = meta.TraceID
	}
	if meta.SpanID != "" {
		out["span_id"] = meta.SpanID
	}
	if meta.UserID != "" {
		out["user_id"] = meta.UserID
	}
	if meta.ActorID != "" {
		out["actor_id"] = meta.ActorID
	}
	return out
}
