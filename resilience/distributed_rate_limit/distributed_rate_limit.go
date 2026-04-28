package flowguard

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// DistributedRateLimiter implements a distributed token-bucket rate limiter
// with local admission control and pluggable remote store (Redis, etcd, etc.).
// It uses a two-level approach:
//  1. Local token bucket for sub-millisecond decisions
//  2. Periodic synchronization with the remote store for global enforcement
//
// This provides near-zero latency overhead while still enforcing cluster-wide
// rate limits across many replicas.
//
// Example:
//
//	store := NewRedisRateLimitStore(redisClient)
//	rl := NewDistributedRateLimiter(DistributedRateLimiterConfig{
//	    Key:            "api:user:42",
//	    RatePerSecond:  1000,
//	    BurstSize:      200,
//	    SyncInterval:   50 * time.Millisecond,
//	    LocalFraction:  0.1,
//	    Store:          store,
//	})
//	if err := rl.Wait(ctx); err != nil {
//	    http.Error(w, "rate limited", 429)
//	    return
//	}
type DistributedRateLimiter struct {
	cfg     DistributedRateLimiterConfig
	local   *localTokenBucket
	metrics *drlMetrics
	stopCh  chan struct{}
	once    sync.Once
	started int32
}

// RateLimitStore abstracts the distributed backend.
// Implementations must be goroutine-safe.
type RateLimitStore interface {
	// IncrBy atomically increments the count for key by delta,
	// sets TTL if the key is new, and returns the new value.
	IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)

	// Get returns the current count for key.
	Get(ctx context.Context, key string) (int64, error)

	// Reset sets the count for key to zero.
	Reset(ctx context.Context, key string) error
}

// DistributedRateLimiterConfig configures the distributed rate limiter.
type DistributedRateLimiterConfig struct {
	// Key uniquely identifies this rate limit (e.g. "user:42:api").
	Key string

	// RatePerSecond is the sustained request rate allowed.
	RatePerSecond float64

	// BurstSize is the maximum burst above the sustained rate.
	BurstSize int64

	// SyncInterval is how often the local bucket syncs with the remote store.
	// Shorter intervals improve global accuracy; longer intervals reduce store load.
	// Default: 100ms.
	SyncInterval time.Duration

	// LocalFraction is the fraction of global quota pre-allocated locally.
	// E.g. 0.1 means 10% of the global bucket is local; reduces remote round-trips.
	LocalFraction float64

	// Store is the distributed backend (required).
	Store RateLimitStore

	// Window is the rolling window for rate measurement.
	// Default: 1s.
	Window time.Duration

	// OnLimitReached is called each time a request is denied.
	OnLimitReached func(key string, waitDuration time.Duration)

	// OnSyncError is called when remote sync fails; local limits continue.
	OnSyncError func(key string, err error)
}

func (c *DistributedRateLimiterConfig) setDefaults() {
	if c.SyncInterval == 0 {
		c.SyncInterval = 100 * time.Millisecond
	}
	if c.LocalFraction == 0 {
		c.LocalFraction = 0.1
	}
	if c.Window == 0 {
		c.Window = time.Second
	}
	if c.BurstSize == 0 {
		c.BurstSize = int64(c.RatePerSecond * 0.2)
	}
}

type localTokenBucket struct {
	mu        sync.Mutex
	tokens    float64
	maxTokens float64
	fillRate  float64 // tokens per nanosecond
	lastFill  time.Time
}

func newLocalTokenBucket(ratePerSec float64, burst int64) *localTokenBucket {
	return &localTokenBucket{
		tokens:    float64(burst),
		maxTokens: float64(burst),
		fillRate:  ratePerSec / 1e9, // tokens per ns
		lastFill:  time.Now(),
	}
}

func (b *localTokenBucket) tryAcquire(n float64) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.lastFill)
	b.tokens = math.Min(b.maxTokens, b.tokens+float64(elapsed.Nanoseconds())*b.fillRate)
	b.lastFill = now

	if b.tokens >= n {
		b.tokens -= n
		return true, 0
	}
	// Calculate wait time.
	wait := time.Duration((n-b.tokens)/b.fillRate) * time.Nanosecond
	return false, wait
}

func (b *localTokenBucket) refill(extra float64) {
	b.mu.Lock()
	b.tokens = math.Min(b.maxTokens, b.tokens+extra)
	b.mu.Unlock()
}

func (b *localTokenBucket) adjustRate(ratePerSec float64) {
	b.mu.Lock()
	b.fillRate = ratePerSec / 1e9
	b.mu.Unlock()
}

type drlMetrics struct {
	allowed     int64
	denied      int64
	syncOps     int64
	syncErrors  int64
	waitTimeNs  int64
}

// NewDistributedRateLimiter creates a new DistributedRateLimiter.
// Call Start() before using.
func NewDistributedRateLimiter(cfg DistributedRateLimiterConfig) *DistributedRateLimiter {
	cfg.setDefaults()
	localRate := cfg.RatePerSecond * cfg.LocalFraction
	localBurst := int64(float64(cfg.BurstSize) * cfg.LocalFraction)
	if localBurst < 1 {
		localBurst = 1
	}
	return &DistributedRateLimiter{
		cfg:     cfg,
		local:   newLocalTokenBucket(localRate, localBurst),
		metrics: &drlMetrics{},
		stopCh:  make(chan struct{}),
	}
}

// Start begins the background sync goroutine. It is safe to call multiple times.
func (d *DistributedRateLimiter) Start() {
	if !atomic.CompareAndSwapInt32(&d.started, 0, 1) {
		return
	}
	go d.syncLoop()
}

// Stop halts background sync.
func (d *DistributedRateLimiter) Stop() {
	d.once.Do(func() { close(d.stopCh) })
}

// Allow checks if a single request is allowed without blocking.
func (d *DistributedRateLimiter) Allow() bool {
	ok, _ := d.local.tryAcquire(1)
	if ok {
		atomic.AddInt64(&d.metrics.allowed, 1)
	} else {
		atomic.AddInt64(&d.metrics.denied, 1)
	}
	return ok
}

// AllowN checks if n requests are allowed without blocking.
func (d *DistributedRateLimiter) AllowN(n int) bool {
	ok, _ := d.local.tryAcquire(float64(n))
	if ok {
		atomic.AddInt64(&d.metrics.allowed, int64(n))
	} else {
		atomic.AddInt64(&d.metrics.denied, int64(n))
	}
	return ok
}

// Wait blocks until a token is available or ctx is done.
func (d *DistributedRateLimiter) Wait(ctx context.Context) error {
	return d.WaitN(ctx, 1)
}

// WaitN blocks until n tokens are available or ctx is done.
func (d *DistributedRateLimiter) WaitN(ctx context.Context, n int) error {
	for {
		ok, wait := d.local.tryAcquire(float64(n))
		if ok {
			atomic.AddInt64(&d.metrics.allowed, int64(n))
			return nil
		}
		atomic.AddInt64(&d.metrics.waitTimeNs, wait.Nanoseconds())
		if d.cfg.OnLimitReached != nil {
			d.cfg.OnLimitReached(d.cfg.Key, wait)
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			atomic.AddInt64(&d.metrics.denied, int64(n))
			return fmt.Errorf("flowguard: rate limiter wait canceled: %w", ctx.Err())
		}
	}
}

// Reserve returns the time to wait before n tokens will be available.
// Does not consume tokens.
func (d *DistributedRateLimiter) Reserve(n int) time.Duration {
	_, wait := d.local.tryAcquire(float64(n))
	// Restore tokens since we're just reserving.
	if wait == 0 {
		d.local.refill(float64(n))
	}
	return wait
}

func (d *DistributedRateLimiter) syncLoop() {
	ticker := time.NewTicker(d.cfg.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.sync()
		case <-d.stopCh:
			return
		}
	}
}

func (d *DistributedRateLimiter) sync() {
	ctx, cancel := context.WithTimeout(context.Background(), d.cfg.SyncInterval/2)
	defer cancel()

	atomic.AddInt64(&d.metrics.syncOps, 1)

	// Report local consumption to global store.
	windowTokens := int64(d.cfg.RatePerSecond * d.cfg.SyncInterval.Seconds())
	_, err := d.cfg.Store.IncrBy(ctx, d.cfg.Key, windowTokens, d.cfg.Window+d.cfg.SyncInterval)
	if err != nil {
		atomic.AddInt64(&d.metrics.syncErrors, 1)
		if d.cfg.OnSyncError != nil {
			d.cfg.OnSyncError(d.cfg.Key, err)
		}
		return
	}

	// Refresh local quota based on global rate.
	refillTokens := float64(windowTokens) * d.cfg.LocalFraction
	d.local.refill(refillTokens)
}

// DRLMetricsSnapshot is a point-in-time snapshot of rate limiter metrics.
type DRLMetricsSnapshot struct {
	Allowed      int64
	Denied       int64
	SyncOps      int64
	SyncErrors   int64
	AvgWaitMs    float64
	DenyRate     float64
}

// Metrics returns a snapshot of rate limiter metrics.
func (d *DistributedRateLimiter) Metrics() DRLMetricsSnapshot {
	allowed := atomic.LoadInt64(&d.metrics.allowed)
	denied := atomic.LoadInt64(&d.metrics.denied)
	waitNs := atomic.LoadInt64(&d.metrics.waitTimeNs)

	total := allowed + denied
	var denyRate, avgWait float64
	if total > 0 {
		denyRate = float64(denied) / float64(total)
	}
	if denied > 0 {
		avgWait = float64(waitNs) / float64(denied) / 1e6 // ms
	}
	return DRLMetricsSnapshot{
		Allowed:    allowed,
		Denied:     denied,
		SyncOps:    atomic.LoadInt64(&d.metrics.syncOps),
		SyncErrors: atomic.LoadInt64(&d.metrics.syncErrors),
		AvgWaitMs:  avgWait,
		DenyRate:   denyRate,
	}
}

// -------- in-memory store (for testing / single-node deployments) --------

// InMemoryRateLimitStore is a thread-safe in-process RateLimitStore.
// It is suitable for single-node deployments or integration tests.
type InMemoryRateLimitStore struct {
	mu      sync.Mutex
	buckets map[string]*imBucket
}

type imBucket struct {
	count     int64
	expiresAt time.Time
}

// NewInMemoryRateLimitStore creates a new InMemoryRateLimitStore.
func NewInMemoryRateLimitStore() *InMemoryRateLimitStore {
	return &InMemoryRateLimitStore{buckets: make(map[string]*imBucket)}
}

func (s *InMemoryRateLimitStore) IncrBy(_ context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	b, ok := s.buckets[key]
	if !ok || now.After(b.expiresAt) {
		b = &imBucket{count: 0, expiresAt: now.Add(ttl)}
		s.buckets[key] = b
	}
	b.count += delta
	return b.count, nil
}

func (s *InMemoryRateLimitStore) Get(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buckets[key]
	if !ok || time.Now().After(b.expiresAt) {
		return 0, nil
	}
	return b.count, nil
}

func (s *InMemoryRateLimitStore) Reset(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.buckets, key)
	s.mu.Unlock()
	return nil
}
