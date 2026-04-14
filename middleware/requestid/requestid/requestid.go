package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type contextKey string

const requestIDContextKey contextKey = "request_id"
const HeaderName = Header

// New creates a request ID.
func New() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WithContext stores request ID in context.
func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDContextKey, id)
}

// FromContext reads request ID from context.
func FromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDContextKey).(string)
	return v
}

// HTTP propagates/generates request IDs for net/http.
func HTTP() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(Header)
			if id == "" {
				id = New()
			}
			w.Header().Set(Header, id)
			ctx := WithContext(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Middleware is compatibility helper that wraps a direct handler.
func Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return HTTP()(next)
}
