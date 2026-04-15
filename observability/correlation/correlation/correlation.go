package gocorrelation

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

type ctxKeyType string

const (
	HeaderName            = "X-Correlation-Id"
	ctxKey     ctxKeyType = "correlation_id"
)

var safeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._\-:/]{1,128}$`)

type Config struct {
	HeaderName      string
	Generator       func() string
	TrustIncomingID bool
	FallbackFromReq func(context.Context) string
}

func Middleware(next http.Handler, cfg Config) http.Handler {
	if next == nil {
		next = http.DefaultServeMux
	}

	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = HeaderName
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""

		if cfg.TrustIncomingID {
			incoming := strings.TrimSpace(r.Header.Get(headerName))
			if isSafeID(incoming) {
				id = incoming
			}
		}

		if id == "" && cfg.FallbackFromReq != nil {
			id = cfg.FallbackFromReq(r.Context())
		}

		if id == "" && cfg.Generator != nil {
			id = cfg.Generator()
		}

		ctx := r.Context()
		if id != "" {
			w.Header().Set(headerName, id)
			ctx = context.WithValue(ctx, ctxKey, id)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func WithID(ctx context.Context, id string) context.Context {
	if !isSafeID(id) {
		return ctx
	}
	return context.WithValue(ctx, ctxKey, id)
}

func ID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey).(string)
	return v
}

func isSafeID(v string) bool {
	return safeIDPattern.MatchString(v)
}
