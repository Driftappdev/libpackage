package resilience

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// TokenBucketConfig configures a TokenBucket rate limiter.
type TokenBucketConfig struct {
	// Name identifies this bucket.
	Name string
	// Rate is the token replenishment rate (tokens per second).
	Rate float64
	// Burst is the maximum number of tokens the bucket can hold.
	Burst float64
	// InitialTokens pre-fills the bucket (default = Burst).
	InitialTokens float64
	// WaitOnEmpty: if true, Wait() blocks instead of returning an error.
	WaitOnEmpty bool
	// MaxWait caps how long Wait() will block (0 = no cap).
	MaxWait time.Duration
	// OnThrottle is called whenever a request is throttled.
	OnThrottle func(name string, waitDuration time.Duration)
	// OnConsume is called on successful token consumption.
	OnConsume func(name string, tokens float64, remaining float64)
}

// TokenBucket is a classic token bucket rate limiter with burst support,
// fractional tokens, and optional blocking wait.
// Thread-safe. Sub-millisecond precision using monotonic clock.
type TokenBucket struct {
	cfg      TokenBucketConfig
	mu       sync.Mutex
	tokens   float64
	lastFill time.Time

	// metrics
	allowed  atomic.Int64
	throttled atomic.Int64
	waited   atomic.Int64
}

// NewTokenBucket creates a new TokenBucket.
func NewTokenBucket(cfg TokenBucketConfig) *TokenBucket {
	if cfg.Rate <= 0 {
		panic("resilience: TokenBucket Rate must be > 0")
	}
	if cfg.Burst <= 0 {
		cfg.Burst = cfg.Rate
	}
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	initial := cfg.InitialTokens
	if initial <= 0 {
		initial = cfg.Burst
	}
	return &TokenBucket{
		cfg:      cfg,
		tokens:   math.Min(initial, cfg.Burst),
		lastFill: time.Now(),
	}
}

// refill adds tokens based on elapsed time since last refill. Must hold mu.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastFill).Seconds()
	tb.tokens = math.Min(tb.cfg.Burst, tb.tokens+elapsed*tb.cfg.Rate)
	tb.lastFill = now
}

// Allow non-blocking attempt to consume n tokens. Returns true if granted.
func (tb *TokenBucket) Allow(n float64) bool {
	if n <= 0 {
		n = 1
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= n {
		tb.tokens -= n
		tb.allowed.Add(1)
		if tb.cfg.OnConsume != nil {
			tb.cfg.OnConsume(tb.cfg.Name, n, tb.tokens)
		}
		return true
	}
	tb.throttled.Add(1)
	return false
}

// Wait blocks until n tokens are available, ctx is cancelled, or MaxWait elapses.
// Returns the wait time and an error if tokens could not be acquired.
func (tb *TokenBucket) Wait(ctx context.Context, n float64) (time.Duration, error) {
	if n <= 0 {
		n = 1
	}

	start := time.Now()

	for {
		tb.mu.Lock()
		tb.refill()
		if tb.tokens >= n {
			tb.tokens -= n
			tb.mu.Unlock()
			waited := time.Since(start)
			tb.allowed.Add(1)
			if waited > 0 {
				tb.waited.Add(1)
			}
			if tb.cfg.OnConsume != nil {
				tb.cfg.OnConsume(tb.cfg.Name, n, tb.tokens)
			}
			return waited, nil
		}

		// Calculate wait time for enough tokens
		deficit := n - tb.tokens
		waitFor := time.Duration(deficit / tb.cfg.Rate * float64(time.Second))
		tb.mu.Unlock()

		if tb.cfg.OnThrottle != nil {
			tb.cfg.OnThrottle(tb.cfg.Name, waitFor)
		}

		if !tb.cfg.WaitOnEmpty {
			tb.throttled.Add(1)
			return 0, fmt.Errorf("token_bucket[%s]: insufficient tokens (need %.2f)", tb.cfg.Name, n)
		}

		// Cap wait time
		sleepFor := waitFor
		if tb.cfg.MaxWait > 0 {
			remaining := tb.cfg.MaxWait - time.Since(start)
			if remaining <= 0 {
				tb.throttled.Add(1)
				return 0, fmt.Errorf("token_bucket[%s]: MaxWait exceeded", tb.cfg.Name)
			}
			if sleepFor > remaining {
				sleepFor = remaining
			}
		}

		select {
		case <-ctx.Done():
			tb.throttled.Add(1)
			return 0, fmt.Errorf("token_bucket[%s]: context cancelled: %w", tb.cfg.Name, ctx.Err())
		case <-time.After(sleepFor):
			// Re-check on next iteration
		}
	}
}

// Reserve returns how long until n tokens will be available (without consuming).
// Returns 0 if tokens are immediately available.
func (tb *TokenBucket) Reserve(n float64) time.Duration {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= n {
		return 0
	}
	deficit := n - tb.tokens
	return time.Duration(deficit / tb.cfg.Rate * float64(time.Second))
}

// Tokens returns the current token count (approximate, not locked).
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}

// SetRate dynamically adjusts the replenishment rate.
func (tb *TokenBucket) SetRate(rate float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill() // apply current rate first
	tb.cfg.Rate = rate
}

// SetBurst dynamically adjusts the burst capacity.
func (tb *TokenBucket) SetBurst(burst float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.cfg.Burst = burst
	if tb.tokens > burst {
		tb.tokens = burst
	}
}

// Stats returns a snapshot.
func (tb *TokenBucket) Stats() TokenBucketStats {
	return TokenBucketStats{
		Name:      tb.cfg.Name,
		Allowed:   tb.allowed.Load(),
		Throttled: tb.throttled.Load(),
		Waited:    tb.waited.Load(),
		Tokens:    tb.Tokens(),
		Rate:      tb.cfg.Rate,
		Burst:     tb.cfg.Burst,
	}
}

// TokenBucketStats is a point-in-time snapshot.
type TokenBucketStats struct {
	Name      string
	Allowed   int64
	Throttled int64
	Waited    int64
	Tokens    float64
	Rate      float64
	Burst     float64
}

// ─────────────────────────────────────────────────────────────────────────────
// MultiKeyTokenBucket: per-key rate limiting (e.g., per-user, per-IP)
// ─────────────────────────────────────────────────────────────────────────────

// MultiKeyTokenBucket manages a pool of token buckets keyed by an identifier.
type MultiKeyTokenBucket struct {
	mu      sync.RWMutex
	buckets map[string]*TokenBucket
	cfg     TokenBucketConfig
	// MaxKeys limits memory usage (LRU eviction not implemented; use with care).
	MaxKeys int
}

// NewMultiKeyTokenBucket creates a multi-key limiter.
func NewMultiKeyTokenBucket(cfg TokenBucketConfig, maxKeys int) *MultiKeyTokenBucket {
	return &MultiKeyTokenBucket{
		buckets: make(map[string]*TokenBucket),
		cfg:     cfg,
		MaxKeys: maxKeys,
	}
}

// Get returns (or creates) the bucket for the given key.
func (m *MultiKeyTokenBucket) Get(key string) *TokenBucket {
	m.mu.RLock()
	b, ok := m.buckets[key]
	m.mu.RUnlock()
	if ok {
		return b
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok = m.buckets[key]; ok {
		return b
	}
	if m.MaxKeys > 0 && len(m.buckets) >= m.MaxKeys {
		// Evict one bucket (first found — production systems should use LRU)
		for k := range m.buckets {
			delete(m.buckets, k)
			break
		}
	}
	cfg := m.cfg
	cfg.Name = fmt.Sprintf("%s[%s]", m.cfg.Name, key)
	b = NewTokenBucket(cfg)
	m.buckets[key] = b
	return b
}

// Allow is shorthand for Get(key).Allow(n).
func (m *MultiKeyTokenBucket) Allow(key string, n float64) bool {
	return m.Get(key).Allow(n)
}

// Wait is shorthand for Get(key).Wait(ctx, n).
func (m *MultiKeyTokenBucket) Wait(ctx context.Context, key string, n float64) (time.Duration, error) {
	return m.Get(key).Wait(ctx, n)
}

// KeyCount returns the number of tracked keys.
func (m *MultiKeyTokenBucket) KeyCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.buckets)
}
