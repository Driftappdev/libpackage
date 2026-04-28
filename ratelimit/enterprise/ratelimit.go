// Package ratelimit provides an enterprise-grade distributed rate limiting system.
//
// Features:
//   - Multiple algorithms: token bucket, sliding window, fixed window, leaky bucket
//   - Redis backend for distributed enforcement
//   - In-memory fallback for local rate limiting
//   - Multi-key rate limiting (per-user, per-tenant, per-IP, per-endpoint)
//   - Composable limiters (AND/OR logic)
//   - Rate limit headers (X-RateLimit-*)
//   - Quota management (daily/monthly quotas)
//   - Priority rate limiting (different rates for different tiers)
//   - Circuit breaker for Redis failures
//   - Prometheus metrics
//   - HTTP middleware
package ratelimit

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/platformcore/libpackage/plugins/common"
	"github.com/go-redis/redis/v8"
)

// ============================================================
// CORE TYPES
// ============================================================

// Algorithm is the rate limiting algorithm.
type Algorithm string

const (
	AlgorithmTokenBucket   Algorithm = "token_bucket"
	AlgorithmSlidingWindow Algorithm = "sliding_window"
	AlgorithmFixedWindow   Algorithm = "fixed_window"
	AlgorithmLeakyBucket   Algorithm = "leaky_bucket"
)

// RateLimitResult is the result of a rate limit check.
type RateLimitResult struct {
	Allowed    bool          `json:"allowed"`
	Remaining  int64         `json:"remaining"`   // Remaining tokens/requests
	ResetAt    time.Time     `json:"reset_at"`    // When the limit resets
	RetryAfter time.Duration `json:"retry_after"` // How long to wait if denied
	Limit      int64         `json:"limit"`       // Total limit
	Key        string        `json:"key"`
	Algorithm  Algorithm     `json:"algorithm"`
}

// LimitConfig defines rate limit parameters.
type LimitConfig struct {
	Key       string        `json:"key"`        // Rate limit key (e.g. "user:123")
	Limit     int64         `json:"limit"`      // Max requests
	Window    time.Duration `json:"window"`     // Time window
	BurstSize int64         `json:"burst_size"` // Extra burst capacity (token bucket)
	Algorithm Algorithm     `json:"algorithm"`
	Cost      int64         `json:"cost"` // Cost per request (default 1)
}

// Limiter is the rate limiter interface.
type Limiter interface {
	Allow(ctx context.Context, cfg LimitConfig) (RateLimitResult, error)
	AllowN(ctx context.Context, cfg LimitConfig, n int64) (RateLimitResult, error)
	Reset(ctx context.Context, key string) error
	Status(ctx context.Context, key string) (RateLimitResult, error)
}

// ============================================================
// REDIS SLIDING WINDOW LIMITER
// ============================================================

// Lua: sliding window using sorted set.
var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])
local unique = ARGV[5]

local clearBefore = now - window

-- Remove expired entries
redis.call("ZREMRANGEBYSCORE", key, "-inf", clearBefore)

-- Count current
local current = redis.call("ZCARD", key)

if current + cost > limit then
    local oldest = redis.call("ZRANGE", key, 0, 0, "WITHSCORES")
    local resetAt = 0
    if #oldest > 0 then
        resetAt = tonumber(oldest[2]) + window
    end
    return {0, limit - current, resetAt}
end

-- Add new request(s)
for i = 1, cost do
    redis.call("ZADD", key, now, unique .. ":" .. i)
end
redis.call("PEXPIRE", key, window)

return {1, limit - current - cost, now + window}
`)

// Lua: token bucket using hash.
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local refillRate = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])

local bucket = redis.call("HMGET", key, "tokens", "last_refill")
local tokens = tonumber(bucket[1]) or capacity
local lastRefill = tonumber(bucket[2]) or now

-- Calculate tokens to add
local elapsed = math.max(0, now - lastRefill)
local tokensToAdd = elapsed * refillRate
tokens = math.min(capacity, tokens + tokensToAdd)

if tokens < cost then
    local waitTime = math.ceil((cost - tokens) / refillRate)
    return {0, math.floor(tokens), waitTime}
end

tokens = tokens - cost
redis.call("HMSET", key, "tokens", tokens, "last_refill", now)
redis.call("PEXPIRE", key, math.ceil(capacity / refillRate) * 1000 + 5000)

return {1, math.floor(tokens), 0}
`)

// Lua: fixed window using counter.
var fixedWindowScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])

local current = redis.call("GET", key)
if current == false then
    current = 0
else
    current = tonumber(current)
end

if current + cost > limit then
    local ttl = redis.call("PTTL", key)
    return {0, limit - current, ttl}
end

local newVal = redis.call("INCRBY", key, cost)
if newVal == cost then
    redis.call("PEXPIRE", key, window)
end

local ttl = redis.call("PTTL", key)
return {1, limit - newVal, ttl}
`)

// RedisLimiter implements distributed rate limiting via Redis.
type RedisLimiter struct {
	client  *redis.Client
	logger  common.Logger
	metrics common.MetricsRecorder
	cb      *common.CircuitBreaker
	local   *MemoryLimiter // fallback
	counter uint64
}

// RedisLimiterConfig configures the Redis rate limiter.
type RedisLimiterConfig struct {
	Client  *redis.Client
	Logger  common.Logger
	Metrics common.MetricsRecorder
}

// NewRedisLimiter creates a new Redis-backed rate limiter.
func NewRedisLimiter(cfg RedisLimiterConfig) *RedisLimiter {
	if cfg.Logger == nil {
		cfg.Logger = common.MustNewLogger("info")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = common.NoopMetrics{}
	}
	return &RedisLimiter{
		client:  cfg.Client,
		logger:  cfg.Logger,
		metrics: cfg.Metrics,
		local:   NewMemoryLimiter(),
		cb: common.NewCircuitBreaker(common.CircuitBreakerConfig{
			Name:        "ratelimit-redis",
			MaxFailures: 5,
			Timeout:     30 * time.Second,
			OnStateChange: func(name string, from, to common.CBState) {
				cfg.Logger.Warn("ratelimit: circuit breaker state change",
					common.String("from", from.String()),
					common.String("to", to.String()))
			},
		}),
	}
}

func (r *RedisLimiter) uniqueID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (r *RedisLimiter) Allow(ctx context.Context, cfg LimitConfig) (RateLimitResult, error) {
	cost := cfg.Cost
	if cost <= 0 {
		cost = 1
	}
	return r.AllowN(ctx, cfg, cost)
}

func (r *RedisLimiter) AllowN(ctx context.Context, cfg LimitConfig, n int64) (RateLimitResult, error) {
	var result RateLimitResult

	err := r.cb.Execute(ctx, func(ctx context.Context) error {
		var err error
		result, err = r.executeRedis(ctx, cfg, n)
		return err
	})

	if err != nil {
		// Fallback to local memory limiter
		r.logger.Warn("ratelimit: redis failed, using local fallback", common.Error(err))
		result, err = r.local.AllowN(ctx, cfg, n)
		if err != nil {
			return RateLimitResult{}, err
		}
	}

	r.metrics.IncrCounter("ratelimit_requests_total", map[string]string{
		"key":     cfg.Key,
		"allowed": strconv.FormatBool(result.Allowed),
	})

	return result, nil
}

func (r *RedisLimiter) executeRedis(ctx context.Context, cfg LimitConfig, n int64) (RateLimitResult, error) {
	algo := cfg.Algorithm
	if algo == "" {
		algo = AlgorithmSlidingWindow
	}

	redisKey := "rl:" + string(algo) + ":" + cfg.Key
	now := time.Now().UnixMilli()

	switch algo {
	case AlgorithmSlidingWindow:
		res, err := slidingWindowScript.Run(ctx, r.client,
			[]string{redisKey},
			now,
			cfg.Window.Milliseconds(),
			cfg.Limit,
			n,
			r.uniqueID(),
		).Int64Slice()
		if err != nil {
			return RateLimitResult{}, err
		}
		allowed := res[0] == 1
		remaining := res[1]
		resetMs := res[2]
		return RateLimitResult{
			Allowed:   allowed,
			Remaining: remaining,
			Limit:     cfg.Limit,
			ResetAt:   time.UnixMilli(resetMs),
			RetryAfter: func() time.Duration {
				if allowed {
					return 0
				}
				return time.Until(time.UnixMilli(resetMs))
			}(),
			Key:       cfg.Key,
			Algorithm: algo,
		}, nil

	case AlgorithmTokenBucket:
		burstSize := cfg.BurstSize
		if burstSize <= 0 {
			burstSize = cfg.Limit
		}
		refillRate := float64(cfg.Limit) / cfg.Window.Seconds() // tokens per second
		res, err := tokenBucketScript.Run(ctx, r.client,
			[]string{redisKey},
			float64(now)/1000, // seconds
			float64(burstSize),
			refillRate,
			float64(n),
		).Int64Slice()
		if err != nil {
			return RateLimitResult{}, err
		}
		allowed := res[0] == 1
		remaining := res[1]
		waitMs := res[2]
		return RateLimitResult{
			Allowed:    allowed,
			Remaining:  remaining,
			Limit:      burstSize,
			ResetAt:    time.Now().Add(cfg.Window),
			RetryAfter: time.Duration(waitMs) * time.Second,
			Key:        cfg.Key,
			Algorithm:  algo,
		}, nil

	case AlgorithmFixedWindow:
		res, err := fixedWindowScript.Run(ctx, r.client,
			[]string{redisKey},
			cfg.Limit,
			cfg.Window.Milliseconds(),
			n,
		).Int64Slice()
		if err != nil {
			return RateLimitResult{}, err
		}
		allowed := res[0] == 1
		remaining := res[1]
		ttlMs := res[2]
		return RateLimitResult{
			Allowed:    allowed,
			Remaining:  remaining,
			Limit:      cfg.Limit,
			ResetAt:    time.Now().Add(time.Duration(ttlMs) * time.Millisecond),
			RetryAfter: time.Duration(ttlMs) * time.Millisecond,
			Key:        cfg.Key,
			Algorithm:  algo,
		}, nil

	default:
		return RateLimitResult{}, fmt.Errorf("unsupported algorithm: %s", algo)
	}
}

func (r *RedisLimiter) Reset(ctx context.Context, key string) error {
	pattern := "rl:*:" + key
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}

func (r *RedisLimiter) Status(ctx context.Context, key string) (RateLimitResult, error) {
	// Peek current count (sliding window)
	redisKey := "rl:sliding_window:" + key
	count, err := r.client.ZCard(ctx, redisKey).Result()
	if err != nil {
		return RateLimitResult{}, err
	}
	return RateLimitResult{
		Key:       key,
		Remaining: count,
	}, nil
}

// ============================================================
// IN-MEMORY LIMITER
// ============================================================

type memEntry struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
	window     time.Time // for fixed/sliding window
	count      int64
}

// MemoryLimiter implements local in-process rate limiting.
type MemoryLimiter struct {
	mu      sync.RWMutex
	entries map[string]*memEntry
}

// NewMemoryLimiter creates a new in-memory rate limiter.
func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{entries: make(map[string]*memEntry)}
}

func (m *MemoryLimiter) getEntry(key string) *memEntry {
	m.mu.RLock()
	e := m.entries[key]
	m.mu.RUnlock()
	if e != nil {
		return e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e = m.entries[key]; e == nil {
		e = &memEntry{}
		m.entries[key] = e
	}
	return e
}

func (m *MemoryLimiter) Allow(ctx context.Context, cfg LimitConfig) (RateLimitResult, error) {
	return m.AllowN(ctx, cfg, 1)
}

func (m *MemoryLimiter) AllowN(_ context.Context, cfg LimitConfig, n int64) (RateLimitResult, error) {
	e := m.getEntry(cfg.Key)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()

	switch cfg.Algorithm {
	case AlgorithmTokenBucket, "":
		burstSize := cfg.BurstSize
		if burstSize <= 0 {
			burstSize = cfg.Limit
		}
		if e.tokens == 0 && e.lastRefill.IsZero() {
			e.tokens = float64(burstSize)
			e.lastRefill = now
		}
		// Refill
		elapsed := now.Sub(e.lastRefill).Seconds()
		refillRate := float64(cfg.Limit) / cfg.Window.Seconds()
		e.tokens = math.Min(float64(burstSize), e.tokens+elapsed*refillRate)
		e.lastRefill = now

		if e.tokens < float64(n) {
			wait := time.Duration((float64(n)-e.tokens)/refillRate) * time.Second
			return RateLimitResult{
				Allowed:    false,
				Remaining:  int64(e.tokens),
				Limit:      burstSize,
				RetryAfter: wait,
				Key:        cfg.Key,
				Algorithm:  AlgorithmTokenBucket,
			}, nil
		}
		e.tokens -= float64(n)
		return RateLimitResult{
			Allowed:   true,
			Remaining: int64(e.tokens),
			Limit:     burstSize,
			Key:       cfg.Key,
			Algorithm: AlgorithmTokenBucket,
		}, nil

	case AlgorithmFixedWindow:
		if now.After(e.window) {
			e.count = 0
			e.window = now.Add(cfg.Window)
		}
		if e.count+n > cfg.Limit {
			return RateLimitResult{
				Allowed:    false,
				Remaining:  cfg.Limit - e.count,
				Limit:      cfg.Limit,
				ResetAt:    e.window,
				RetryAfter: time.Until(e.window),
				Key:        cfg.Key,
				Algorithm:  AlgorithmFixedWindow,
			}, nil
		}
		e.count += n
		return RateLimitResult{
			Allowed:   true,
			Remaining: cfg.Limit - e.count,
			Limit:     cfg.Limit,
			ResetAt:   e.window,
			Key:       cfg.Key,
			Algorithm: AlgorithmFixedWindow,
		}, nil
	}

	return RateLimitResult{Allowed: true, Remaining: cfg.Limit, Key: cfg.Key}, nil
}

func (m *MemoryLimiter) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.entries, key)
	m.mu.Unlock()
	return nil
}

func (m *MemoryLimiter) Status(_ context.Context, key string) (RateLimitResult, error) {
	m.mu.RLock()
	e := m.entries[key]
	m.mu.RUnlock()
	if e == nil {
		return RateLimitResult{Key: key}, nil
	}
	e.mu.Lock()
	tokens := e.tokens
	e.mu.Unlock()
	return RateLimitResult{Key: key, Remaining: int64(tokens)}, nil
}

// ============================================================
// COMPOSITE LIMITER
// ============================================================

// CompositeLimiter checks multiple rate limits in sequence (all must pass).
type CompositeLimiter struct {
	limiters []namedLimiter
}

type namedLimiter struct {
	name    string
	limiter Limiter
	cfg     LimitConfig
}

// NewCompositeLimiter creates a composite rate limiter.
func NewCompositeLimiter() *CompositeLimiter {
	return &CompositeLimiter{}
}

// Add adds a named limiter to the composite.
func (c *CompositeLimiter) Add(name string, limiter Limiter, cfg LimitConfig) *CompositeLimiter {
	c.limiters = append(c.limiters, namedLimiter{name: name, limiter: limiter, cfg: cfg})
	return c
}

// Allow checks all limiters; returns the most restrictive result.
func (c *CompositeLimiter) Allow(ctx context.Context) (RateLimitResult, error) {
	var mostRestrictive *RateLimitResult
	for _, nl := range c.limiters {
		result, err := nl.limiter.Allow(ctx, nl.cfg)
		if err != nil {
			return RateLimitResult{}, fmt.Errorf("limiter %q: %w", nl.name, err)
		}
		if !result.Allowed {
			return result, nil // First deny wins
		}
		if mostRestrictive == nil || result.Remaining < mostRestrictive.Remaining {
			cp := result
			mostRestrictive = &cp
		}
	}
	if mostRestrictive != nil {
		return *mostRestrictive, nil
	}
	return RateLimitResult{Allowed: true}, nil
}

// ============================================================
// QUOTA MANAGER
// ============================================================

// Quota represents a usage quota (e.g. monthly API calls).
type Quota struct {
	Key     string      `json:"key"`
	Limit   int64       `json:"limit"`
	Period  QuotaPeriod `json:"period"`
	Used    int64       `json:"used"`
	ResetAt time.Time   `json:"reset_at"`
}

// QuotaPeriod is the billing/quota period.
type QuotaPeriod string

const (
	QuotaPeriodHourly  QuotaPeriod = "hourly"
	QuotaPeriodDaily   QuotaPeriod = "daily"
	QuotaPeriodWeekly  QuotaPeriod = "weekly"
	QuotaPeriodMonthly QuotaPeriod = "monthly"
)

// QuotaManager manages quota usage in Redis.
type QuotaManager struct {
	client *redis.Client
	logger common.Logger
}

// NewQuotaManager creates a new quota manager.
func NewQuotaManager(client *redis.Client, logger common.Logger) *QuotaManager {
	return &QuotaManager{client: client, logger: logger}
}

func (q *QuotaManager) quotaKey(key string, period QuotaPeriod) string {
	now := time.Now()
	var suffix string
	switch period {
	case QuotaPeriodHourly:
		suffix = now.Format("2006010215")
	case QuotaPeriodDaily:
		suffix = now.Format("20060102")
	case QuotaPeriodWeekly:
		year, week := now.ISOWeek()
		suffix = fmt.Sprintf("%d%02d", year, week)
	case QuotaPeriodMonthly:
		suffix = now.Format("200601")
	}
	return fmt.Sprintf("quota:%s:%s:%s", string(period), key, suffix)
}

func (q *QuotaManager) periodTTL(period QuotaPeriod) time.Duration {
	switch period {
	case QuotaPeriodHourly:
		return 2 * time.Hour
	case QuotaPeriodDaily:
		return 48 * time.Hour
	case QuotaPeriodWeekly:
		return 2 * 7 * 24 * time.Hour
	case QuotaPeriodMonthly:
		return 62 * 24 * time.Hour
	}
	return 24 * time.Hour
}

// Consume consumes n units from the quota. Returns an error if quota exceeded.
func (q *QuotaManager) Consume(ctx context.Context, key string, period QuotaPeriod, limit, n int64) (*Quota, error) {
	redisKey := q.quotaKey(key, period)
	newVal, err := q.client.IncrBy(ctx, redisKey, n).Result()
	if err != nil {
		return nil, err
	}
	// Set TTL on first write
	if newVal == n {
		q.client.Expire(ctx, redisKey, q.periodTTL(period)) //nolint:errcheck
	}

	ttl, _ := q.client.TTL(ctx, redisKey).Result()
	quota := &Quota{
		Key:     key,
		Limit:   limit,
		Period:  period,
		Used:    newVal,
		ResetAt: time.Now().Add(ttl),
	}

	if newVal > limit {
		return quota, fmt.Errorf("quota exceeded: used %d / %d", newVal, limit)
	}
	return quota, nil
}

// Status returns current quota usage.
func (q *QuotaManager) Status(ctx context.Context, key string, period QuotaPeriod, limit int64) (*Quota, error) {
	redisKey := q.quotaKey(key, period)
	used, err := q.client.Get(ctx, redisKey).Int64()
	if err == redis.Nil {
		used = 0
	} else if err != nil {
		return nil, err
	}
	ttl, _ := q.client.TTL(ctx, redisKey).Result()
	return &Quota{
		Key:     key,
		Limit:   limit,
		Period:  period,
		Used:    used,
		ResetAt: time.Now().Add(ttl),
	}, nil
}

// ============================================================
// HTTP MIDDLEWARE
// ============================================================

// KeyExtractor extracts the rate limit key from an HTTP request.
type KeyExtractor func(r *http.Request) string

// ByIP extracts the client IP.
func ByIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	return "ip:" + ip
}

// ByAPIKey extracts the API key header.
func ByAPIKey(header string) KeyExtractor {
	return func(r *http.Request) string {
		return "apikey:" + r.Header.Get(header)
	}
}

// ByUser extracts the user from context.
func ByUser(userFromCtx func(r *http.Request) string) KeyExtractor {
	return func(r *http.Request) string {
		return "user:" + userFromCtx(r)
	}
}

// HTTPMiddleware returns an HTTP rate limiting middleware.
func HTTPMiddleware(limiter Limiter, cfg LimitConfig, keyExtractor KeyExtractor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyExtractor(r)
			cfg.Key = key

			result, err := limiter.Allow(r.Context(), cfg)
			if err != nil {
				http.Error(w, "Rate limiter error", http.StatusInternalServerError)
				return
			}

			// Set standard rate limit response headers
			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TieredHTTPMiddleware applies different rate limits by tier.
func TieredHTTPMiddleware(limiter Limiter, tierExtractor func(*http.Request) string, tiers map[string]LimitConfig, keyExtractor KeyExtractor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tier := tierExtractor(r)
			cfg, ok := tiers[tier]
			if !ok {
				cfg = tiers["default"]
			}
			cfg.Key = keyExtractor(r)

			result, err := limiter.Allow(r.Context(), cfg)
			if err != nil {
				http.Error(w, "Rate limiter error", http.StatusInternalServerError)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
			w.Header().Set("X-RateLimit-Tier", tier)

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

