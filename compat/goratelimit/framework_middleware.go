package goratelimit

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ByRemoteAddr keeps compatibility with middleware/ratelimit extractor naming.
func ByRemoteAddr(r *http.Request) string { return r.RemoteAddr }

// HTTP is a standard-library middleware built around Engine.
func HTTP(l *Engine, extract KeyExtractor) func(http.Handler) http.Handler {
	if extract == nil {
		extract = ByRemoteAddr
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l == nil {
				next.ServeHTTP(w, r)
				return
			}
			res, err := l.Allow(r.Context(), Key{Namespace: "http", Identity: extract(r)})
			if err == nil || errors.Is(err, ErrLimited) {
				w.Header().Set(HeaderLimit, strconv.Itoa(res.Limit))
				w.Header().Set(HeaderRemaining, strconv.Itoa(res.Remaining))
				if res.ResetAfter > 0 {
					w.Header().Set(HeaderReset, strconv.FormatInt(time.Now().Add(res.ResetAfter).Unix(), 10))
				}
			}
			if errors.Is(err, ErrLimited) {
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

// Gin is a gin middleware compatible with middleware/ratelimit/gin.
func Gin(l *Engine, extract KeyExtractor) gin.HandlerFunc {
	if extract == nil {
		extract = ByRemoteAddr
	}
	return func(c *gin.Context) {
		if l == nil {
			c.Next()
			return
		}
		res, err := l.Allow(c.Request.Context(), Key{Namespace: "http", Identity: extract(c.Request)})
		if err == nil || errors.Is(err, ErrLimited) {
			c.Header(HeaderLimit, strconv.Itoa(res.Limit))
			c.Header(HeaderRemaining, strconv.Itoa(res.Remaining))
		}
		if errors.Is(err, ErrLimited) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limited"})
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Next()
	}
}

type GRPCExtractor func(ctx context.Context, method string) string

// UnaryServerInterceptor is a grpc unary interceptor built around Engine.
func UnaryServerInterceptor(l *Engine, extract GRPCExtractor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if l == nil {
			return handler(ctx, req)
		}
		subject := info.FullMethod
		if extract != nil {
			subject = extract(ctx, info.FullMethod)
		}
		_, err := l.Allow(ctx, Key{Namespace: "grpc", Identity: subject})
		if errors.Is(err, ErrLimited) {
			return nil, status.Error(codes.ResourceExhausted, "rate limited")
		}
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return handler(ctx, req)
	}
}
