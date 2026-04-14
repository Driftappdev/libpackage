package gotrace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type ctxKeyType string

const (
	traceKey  ctxKeyType = "trace_id"
	parentKey ctxKeyType = "parent_span_id"
	spanKey   ctxKeyType = "span_id"
)

const (
	HeaderTraceID  = "X-Trace-Id"
	HeaderParentID = "X-Parent-Span-Id"
	HeaderSpanID   = "X-Span-Id"
)

type Config struct {
	TrustIncoming bool
}

func Middleware(next http.Handler, cfg Config) http.Handler {
	if next == nil {
		next = http.DefaultServeMux
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := ""
		parentID := ""
		spanID := NewSpanID()

		if cfg.TrustIncoming {
			traceID = strings.TrimSpace(r.Header.Get(HeaderTraceID))
			parentID = strings.TrimSpace(r.Header.Get(HeaderSpanID))
		}

		if traceID == "" {
			traceID = NewTraceID()
		}

		ctx := context.WithValue(r.Context(), traceKey, traceID)
		ctx = context.WithValue(ctx, parentKey, parentID)
		ctx = context.WithValue(ctx, spanKey, spanID)

		w.Header().Set(HeaderTraceID, traceID)
		w.Header().Set(HeaderSpanID, spanID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceKey).(string)
	return v
}

func ParentSpanID(ctx context.Context) string {
	v, _ := ctx.Value(parentKey).(string)
	return v
}

func SpanID(ctx context.Context) string {
	v, _ := ctx.Value(spanKey).(string)
	return v
}

func NewTraceID() string {
	return randomHex(16)
}

func NewSpanID() string {
	return randomHex(8)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(buf)
}
