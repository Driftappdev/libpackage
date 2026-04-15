package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type contextKey string

const correlationKey contextKey = "midul.correlation.id"
const HeaderName = "X-Correlation-ID"

func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}
func IDFromContext(ctx context.Context) string { v, _ := ctx.Value(correlationKey).(string); return v }

func WithContext(ctx context.Context, id string) context.Context {
	return WithID(ctx, id)
}

func FromContext(ctx context.Context) string {
	return IDFromContext(ctx)
}

func Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderName)
		if id == "" {
			id = randomID()
		}
		w.Header().Set(HeaderName, id)
		next.ServeHTTP(w, r.WithContext(WithID(r.Context(), id)))
	})
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "corr-fallback"
	}
	return hex.EncodeToString(b[:])
}
