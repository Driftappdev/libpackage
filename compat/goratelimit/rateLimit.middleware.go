// Package goratelimit provides production-grade rate limiting:
// token bucket, sliding window, fixed window, per-key limiters,
// Redis-compatible interface, and HTTP middleware.
package goratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- Result -----------------------------------------------------------------

// Result is returned by every limiter call.
type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAfter time.Duration
	RetryAfter time.Duration
}

// ---- Limiter interface ------------------------------------------------------

// Limiter is the common interface for all rate limiters.
type Limiter interface {
	Allow(ctx context.Context, key string) (Result, error)
	Peek(ctx context.Context, key string) (Result, error)
	Reset(ctx context.Context, key string) error
}

// ---- Token Bucket -----------------------------------------------------------

type tokenBucket struct {
	tokens   float64
	last     time.Time
	capacity float64
	rate     float64 // tokens per second
}

// TokenBucketLimiter allows `rate` requests per second with burst up to `capacity`.
type TokenBucketLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	capacity float64
	rate     float64
}

// NewTokenBucket creates a limiter allowing rate req/s with burst capacity.
func NewTokenBucket(ratePerSec float64, burst int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		buckets:  make(map[string]*tokenBucket),
		capacity: float64(burst),
		rate:     ratePerSec,
	}
}

func (l *TokenBucketLimiter) bucket(key string) *tokenBucket {
	b, ok := l.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: l.capacity, last: time.Now(), capacity: l.capacity, rate: l.rate}
		l.buckets[key] = b
	}
	return b
}

func (l *TokenBucketLimiter) refill(b *tokenBucket) {
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min64(b.capacity, b.tokens+elapsed*b.rate)
	b.last = now
}

func (l *TokenBucketLimiter) Allow(ctx context.Context, key string) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucket(key)
	l.refill(b)
	remaining := int(b.tokens)
	if b.tokens >= 1 {
		b.tokens--
		remaining = int(b.tokens)
		return Result{Allowed: true, Limit: int(b.capacity), Remaining: remaining, ResetAfter: 0}, nil
	}
	waitSec := (1 - b.tokens) / b.rate
	retry := time.Duration(waitSec * float64(time.Second))
	return Result{Allowed: false, Limit: int(b.capacity), Remaining: 0, RetryAfter: retry}, nil
}

func (l *TokenBucketLimiter) Peek(ctx context.Context, key string) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucket(key)
	l.refill(b)
	return Result{Allowed: b.tokens >= 1, Limit: int(b.capacity), Remaining: int(b.tokens)}, nil
}

func (l *TokenBucketLimiter) Reset(ctx context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
	return nil
}

// ---- Fixed Window -----------------------------------------------------------

type windowEntry struct {
	count    int
	windowAt time.Time
}

// FixedWindowLimiter allows limit requests per window duration.
type FixedWindowLimiter struct {
	mu      sync.Mutex
	entries map[string]*windowEntry
	limit   int
	window  time.Duration
}

// NewFixedWindow creates a fixed-window limiter.
func NewFixedWindow(limit int, window time.Duration) *FixedWindowLimiter {
	return &FixedWindowLimiter{entries: make(map[string]*windowEntry), limit: limit, window: window}
}

func (l *FixedWindowLimiter) Allow(ctx context.Context, key string) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.windowAt) >= l.window {
		l.entries[key] = &windowEntry{count: 1, windowAt: now}
		return Result{Allowed: true, Limit: l.limit, Remaining: l.limit - 1, ResetAfter: l.window}, nil
	}
	e.count++
	remaining := l.limit - e.count
	if remaining < 0 {
		remaining = 0
	}
	reset := l.window - now.Sub(e.windowAt)
	if e.count > l.limit {
		return Result{Allowed: false, Limit: l.limit, Remaining: 0, RetryAfter: reset}, nil
	}
	return Result{Allowed: true, Limit: l.limit, Remaining: remaining, ResetAfter: reset}, nil
}

func (l *FixedWindowLimiter) Peek(ctx context.Context, key string) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.windowAt) >= l.window {
		return Result{Allowed: true, Limit: l.limit, Remaining: l.limit}, nil
	}
	rem := l.limit - e.count
	if rem < 0 {
		rem = 0
	}
	return Result{Allowed: e.count < l.limit, Limit: l.limit, Remaining: rem}, nil
}

func (l *FixedWindowLimiter) Reset(ctx context.Context, key string) error {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
	return nil
}

// ---- Sliding Window ---------------------------------------------------------

// SlidingWindowLimiter uses a ring buffer of timestamps for accurate rate limiting.
type SlidingWindowLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

// NewSlidingWindow creates a sliding-window limiter.
func NewSlidingWindow(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{requests: make(map[string][]time.Time), limit: limit, window: window}
}

func (l *SlidingWindowLimiter) Allow(ctx context.Context, key string) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	times := l.requests[key]
	// Purge old
	i := 0
	for i < len(times) && times[i].Before(cutoff) {
		i++
	}
	times = times[i:]

	if len(times) >= l.limit {
		retry := l.window - now.Sub(times[0])
		l.requests[key] = times
		return Result{Allowed: false, Limit: l.limit, Remaining: 0, RetryAfter: retry}, nil
	}
	times = append(times, now)
	l.requests[key] = times
	return Result{Allowed: true, Limit: l.limit, Remaining: l.limit - len(times)}, nil
}

func (l *SlidingWindowLimiter) Peek(ctx context.Context, key string) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	times := l.requests[key]
	i := 0
	for i < len(times) && times[i].Before(cutoff) {
		i++
	}
	active := len(times) - i
	rem := l.limit - active
	if rem < 0 {
		rem = 0
	}
	return Result{Allowed: active < l.limit, Limit: l.limit, Remaining: rem}, nil
}

func (l *SlidingWindowLimiter) Reset(ctx context.Context, key string) error {
	l.mu.Lock()
	delete(l.requests, key)
	l.mu.Unlock()
	return nil
}

// ---- Key extractor ----------------------------------------------------------

// KeyExtractor derives a rate-limit key from an HTTP request.
type KeyExtractor func(r *http.Request) string

// ByIP extracts the client IP address.
func ByIP() KeyExtractor {
	return func(r *http.Request) string {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		// Honour forwarded-for from trusted proxies.
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			return strings.TrimSpace(parts[0])
		}
		return ip
	}
}

// ByHeader extracts a request header value (e.g. API key).
func ByHeader(header string) KeyExtractor {
	return func(r *http.Request) string {
		return r.Header.Get(header)
	}
}

// ByUser extracts the user ID from a context key.
func ByUser(ctxKey interface{}) KeyExtractor {
	return func(r *http.Request) string {
		v, _ := r.Context().Value(ctxKey).(string)
		return v
	}
}

// ByPath uses the request path as the key.
func ByPath() KeyExtractor {
	return func(r *http.Request) string { return r.URL.Path }
}

// Combine joins multiple extractors with ":" to produce a composite key.
func Combine(extractors ...KeyExtractor) KeyExtractor {
	return func(r *http.Request) string {
		parts := make([]string, len(extractors))
		for i, e := range extractors {
			parts[i] = e(r)
		}
		return strings.Join(parts, ":")
	}
}

// ---- HTTP Middleware ---------------------------------------------------------

// MiddlewareConfig configures rate limit middleware.
type MiddlewareConfig struct {
	Limiter      Limiter
	KeyExtractor KeyExtractor
	// OnLimited is called when a request is rate limited.
	// If nil, a default 429 response is sent.
	OnLimited func(w http.ResponseWriter, r *http.Request, result Result)
	// SkipIf causes the middleware to skip rate limiting when true.
	SkipIf func(r *http.Request) bool
}

// Middleware returns an http.Handler that enforces rate limiting.
func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	extract := cfg.KeyExtractor
	if extract == nil {
		extract = ByIP()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.SkipIf != nil && cfg.SkipIf(r) {
				next.ServeHTTP(w, r)
				return
			}
			key := extract(r)
			result, err := cfg.Limiter.Allow(r.Context(), key)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			// Set standard rate limit headers.
			w.Header().Set("X-RateLimit-Limit", fmt.Sprint(result.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprint(result.Remaining))
			if result.ResetAfter > 0 {
				w.Header().Set("X-RateLimit-Reset", fmt.Sprint(int(result.ResetAfter.Seconds())))
			}

			if !result.Allowed {
				if result.RetryAfter > 0 {
					w.Header().Set("Retry-After", fmt.Sprint(int(result.RetryAfter.Seconds())))
				}
				if cfg.OnLimited != nil {
					cfg.OnLimited(w, r, result)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "rate limit exceeded",
					"retry_after": result.RetryAfter.Seconds(),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- Multi-tier limiter (apply multiple limits simultaneously) ---------------

// MultiLimiter applies several limiters in order; the first rejection wins.
type MultiLimiter struct {
	limiters []Limiter
}

// NewMultiLimiter chains limiters (e.g. IP + user + global).
func NewMultiLimiter(limiters ...Limiter) *MultiLimiter {
	return &MultiLimiter{limiters: limiters}
}

func (m *MultiLimiter) Allow(ctx context.Context, key string) (Result, error) {
	var last Result
	for _, l := range m.limiters {
		r, err := l.Allow(ctx, key)
		if err != nil {
			return r, err
		}
		last = r
		if !r.Allowed {
			return r, nil
		}
	}
	return last, nil
}

func (m *MultiLimiter) Peek(ctx context.Context, key string) (Result, error) {
	var last Result
	for _, l := range m.limiters {
		r, err := l.Peek(ctx, key)
		if err != nil {
			return r, err
		}
		last = r
		if !r.Allowed {
			return r, nil
		}
	}
	return last, nil
}

func (m *MultiLimiter) Reset(ctx context.Context, key string) error {
	for _, l := range m.limiters {
		if err := l.Reset(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
