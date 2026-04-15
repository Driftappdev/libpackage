package ratelimit

import (
	"errors"
	"net/http"
	"strconv"

	core "github.com/driftappdev/libpackage/ratelimit"
)

// HTTP provides standard-library rate-limit middleware.
func HTTP(l *core.Limiter, extract KeyExtractor) func(http.Handler) http.Handler {
	if extract == nil {
		extract = ByRemoteAddr
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l == nil {
				next.ServeHTTP(w, r)
				return
			}
			res, err := l.Allow(r.Context(), core.Key{Namespace: "http", Identity: extract(r)})
			if err == nil || errors.Is(err, core.ErrLimited) {
				w.Header().Set(HeaderLimit, strconv.FormatInt(res.Limit, 10))
				w.Header().Set(HeaderRemaining, strconv.FormatInt(res.Remaining, 10))
				w.Header().Set(HeaderReset, strconv.FormatInt(res.ResetAt.Unix(), 10))
			}
			if errors.Is(err, core.ErrLimited) {
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
