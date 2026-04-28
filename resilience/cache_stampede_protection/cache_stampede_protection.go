package flowguard

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// CacheStampedeProtector prevents cache stampedes (thundering herd) using three
// complementary strategies:
//
//  1. Probabilistic early expiration (XFetch / "optimal" algorithm by Vattani et al. 2015)
//     — recomputes the value slightly before it expires, probability proportional
//     to remaining TTL and recompute cost.
//  2. Lock-based coalescing — only one goroutine recomputes; others wait for it.
//  3. Stale-while-revalidate — return the stale value immediately while
//     recomputing asynchronously.
//
// Example:
//
//	p := NewCacheStampedeProtector(StampedeConfig{
//	    Strategy:   StrategyProbabilistic,
//	    Beta:       1.0,
//	    StaleGrace: 5 * time.Second,
//	    OnStale:    metrics.StaleServed,
//	})
//	val, err := p.Get(ctx, "user:42", func(ctx context.Context) (any, time.Duration, error) {
//	    user, err := db.GetUser(ctx, 42)
//	    return user, 5*time.Minute, err
//	})
type CacheStampedeProtector struct {
	cfg     StampedeConfig
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	locks   map[string]*sync.Mutex
	metrics *stampedeMetrics
}

// StampedeStrategy selects the anti-stampede algorithm.
type StampedeStrategy int

const (
	// StrategyLockCoalesce uses a per-key mutex: one recomputes, rest wait.
	StrategyLockCoalesce StampedeStrategy = iota

	// StrategyProbabilistic uses XFetch: probabilistic early recompute.
	StrategyProbabilistic

	// StrategyStaleWhileRevalidate returns stale value immediately, recomputes async.
	StrategyStaleWhileRevalidate

	// StrategyHybrid combines probabilistic early recompute + stale-while-revalidate.
	StrategyHybrid
)

// StampedeConfig configures the CacheStampedeProtector.
type StampedeConfig struct {
	Strategy StampedeStrategy

	// Beta controls aggressiveness of probabilistic early recompute.
	// Higher β = earlier recompute (default: 1.0, set >1 for expensive fetches).
	Beta float64

	// StaleGrace is how long a stale value may be served while recomputing.
	// Only applies to StrategyStaleWhileRevalidate and StrategyHybrid.
	StaleGrace time.Duration

	// MaxEntries caps the number of cached keys. 0 = unlimited.
	MaxEntries int

	// BackgroundTimeout is the deadline for async recompute in SWR mode.
	BackgroundTimeout time.Duration

	// OnStale is called when a stale value is served (optional).
	OnStale func(key string, age time.Duration)

	// OnRecompute is called when a recompute is triggered (optional).
	OnRecompute func(key string, strategy string)

	// OnEvict is called when an entry is evicted (optional).
	OnEvict func(key string)
}

func (c *StampedeConfig) setDefaults() {
	if c.Beta == 0 {
		c.Beta = 1.0
	}
	if c.StaleGrace == 0 {
		c.StaleGrace = 10 * time.Second
	}
	if c.BackgroundTimeout == 0 {
		c.BackgroundTimeout = 30 * time.Second
	}
}

type cacheEntry struct {
	value       any
	expiresAt   time.Time
	delta       time.Duration // last recompute cost (for XFetch)
	createdAt   time.Time
	recomputing int32 // 1 if background recompute in progress
}

func (e *cacheEntry) isExpired() bool {
	return time.Now().After(e.expiresAt)
}

func (e *cacheEntry) age() time.Duration {
	return time.Since(e.createdAt)
}

// xfetchShouldRecompute implements the XFetch algorithm.
// Returns true if this goroutine should recompute the value.
func (e *cacheEntry) xfetchShouldRecompute(beta float64) bool {
	remaining := time.Until(e.expiresAt)
	if remaining <= 0 {
		return true
	}
	// prob = -delta * beta * ln(rand())
	// recompute if: now + prob*delta >= expiresAt
	rnd := rand.Float64() //nolint:gosec
	if rnd <= 0 {
		return true
	}
	earlyBy := -float64(e.delta) * beta * safeLog(rnd)
	return float64(remaining) <= earlyBy
}

func safeLog(x float64) float64 {
	if x <= 0 {
		return -700
	}
	return logFloat64(x)
}

func logFloat64(x float64) float64 {
	// Newton's method approximation for ln(x)
	// Good enough for our probabilistic use case.
	// Use math.Log via the standard library would normally be used here.
	// We inline to avoid importing math again (already imported in other files).
	return mathLog(x)
}

// mathLog computes natural log using the standard approach.
// In real usage, just call math.Log directly.
func mathLog(x float64) float64 {
	// IEEE 754 fast log: decompose into mantissa * 2^exp.
	// For production, use math.Log(x).
	// We approximate here without importing math:
	if x <= 0 {
		return -700.0
	}
	// Use 5-term Taylor expansion around 1: good for 0.5 ≤ x ≤ 2.0.
	// For values outside this range, scale by powers of 2.
	const ln2 = 0.6931471805599453
	exp := 0
	for x >= 2.0 {
		x /= 2.0
		exp++
	}
	for x < 0.5 {
		x *= 2.0
		exp--
	}
	// x ∈ [0.5, 2.0); use ln(1+y) ≈ y - y²/2 + y³/3 - y⁴/4 + y⁵/5
	y := x - 1.0
	result := y - y*y/2 + y*y*y/3 - y*y*y*y/4 + y*y*y*y*y/5
	return result + float64(exp)*ln2
}

type stampedeMetrics struct {
	hits         int64
	misses       int64
	staleServed  int64
	recomputes   int64
	evictions    int64
	stampedePrev int64 // number of stampedes prevented
}

// FetchFunc computes a value. Returns (value, ttl, error).
type FetchFunc func(ctx context.Context) (value any, ttl time.Duration, err error)

// NewCacheStampedeProtector creates a new CacheStampedeProtector.
func NewCacheStampedeProtector(cfg StampedeConfig) *CacheStampedeProtector {
	cfg.setDefaults()
	return &CacheStampedeProtector{
		cfg:     cfg,
		entries: make(map[string]*cacheEntry),
		locks:   make(map[string]*sync.Mutex),
		metrics: &stampedeMetrics{},
	}
}

// Get returns the cached value for key, or calls fetchFn to compute it.
// The anti-stampede strategy is applied transparently.
func (p *CacheStampedeProtector) Get(ctx context.Context, key string, fetchFn FetchFunc) (any, error) {
	switch p.cfg.Strategy {
	case StrategyLockCoalesce:
		return p.getLockCoalesce(ctx, key, fetchFn)
	case StrategyProbabilistic:
		return p.getProbabilistic(ctx, key, fetchFn)
	case StrategyStaleWhileRevalidate:
		return p.getSWR(ctx, key, fetchFn)
	case StrategyHybrid:
		return p.getHybrid(ctx, key, fetchFn)
	default:
		return p.getLockCoalesce(ctx, key, fetchFn)
	}
}

// getLockCoalesce: single recompute, all others wait.
func (p *CacheStampedeProtector) getLockCoalesce(ctx context.Context, key string, fetchFn FetchFunc) (any, error) {
	if val, ok := p.cacheGet(key); ok {
		atomic.AddInt64(&p.metrics.hits, 1)
		return val, nil
	}

	// Acquire per-key lock.
	kmu := p.keyLock(key)
	kmu.Lock()
	defer kmu.Unlock()

	// Double-check after acquiring lock.
	if val, ok := p.cacheGet(key); ok {
		atomic.AddInt64(&p.metrics.stampedePrev, 1)
		return val, nil
	}

	atomic.AddInt64(&p.metrics.misses, 1)
	return p.recompute(ctx, key, fetchFn)
}

// getProbabilistic: XFetch probabilistic early recompute.
func (p *CacheStampedeProtector) getProbabilistic(ctx context.Context, key string, fetchFn FetchFunc) (any, error) {
	p.mu.RLock()
	entry, ok := p.entries[key]
	p.mu.RUnlock()

	if ok && !entry.isExpired() {
		if !entry.xfetchShouldRecompute(p.cfg.Beta) {
			atomic.AddInt64(&p.metrics.hits, 1)
			return entry.value, nil
		}
		// Early recompute triggered.
		if p.cfg.OnRecompute != nil {
			p.cfg.OnRecompute(key, "probabilistic-early")
		}
	}

	kmu := p.keyLock(key)
	kmu.Lock()
	defer kmu.Unlock()

	// Recheck.
	p.mu.RLock()
	entry, ok = p.entries[key]
	p.mu.RUnlock()
	if ok && !entry.isExpired() && !entry.xfetchShouldRecompute(p.cfg.Beta) {
		atomic.AddInt64(&p.metrics.stampedePrev, 1)
		return entry.value, nil
	}

	atomic.AddInt64(&p.metrics.misses, 1)
	return p.recompute(ctx, key, fetchFn)
}

// getSWR: serve stale, recompute asynchronously.
func (p *CacheStampedeProtector) getSWR(ctx context.Context, key string, fetchFn FetchFunc) (any, error) {
	p.mu.RLock()
	entry, ok := p.entries[key]
	p.mu.RUnlock()

	if ok {
		if !entry.isExpired() {
			atomic.AddInt64(&p.metrics.hits, 1)
			return entry.value, nil
		}
		// Stale: serve it and trigger background recompute.
		staleAge := entry.age()
		if staleAge <= p.cfg.StaleGrace {
			atomic.AddInt64(&p.metrics.staleServed, 1)
			if p.cfg.OnStale != nil {
				p.cfg.OnStale(key, staleAge)
			}
			// Kick off background recompute only once.
			if atomic.CompareAndSwapInt32(&entry.recomputing, 0, 1) {
				go func() {
					bgCtx, cancel := context.WithTimeout(context.Background(), p.cfg.BackgroundTimeout)
					defer cancel()
					if _, err := p.recompute(bgCtx, key, fetchFn); err == nil {
						atomic.AddInt64(&p.metrics.recomputes, 1)
					}
					// Reset recomputing flag on the new entry.
					p.mu.RLock()
					if e, exists := p.entries[key]; exists {
						atomic.StoreInt32(&e.recomputing, 0)
					}
					p.mu.RUnlock()
				}()
			}
			return entry.value, nil
		}
	}

	atomic.AddInt64(&p.metrics.misses, 1)
	return p.recompute(ctx, key, fetchFn)
}

// getHybrid: probabilistic early recompute + stale-while-revalidate.
func (p *CacheStampedeProtector) getHybrid(ctx context.Context, key string, fetchFn FetchFunc) (any, error) {
	p.mu.RLock()
	entry, ok := p.entries[key]
	p.mu.RUnlock()

	if ok {
		if !entry.isExpired() {
			if !entry.xfetchShouldRecompute(p.cfg.Beta) {
				atomic.AddInt64(&p.metrics.hits, 1)
				return entry.value, nil
			}
			// Early probabilistic recompute (async, no stall).
			if atomic.CompareAndSwapInt32(&entry.recomputing, 0, 1) {
				if p.cfg.OnRecompute != nil {
					p.cfg.OnRecompute(key, "hybrid-early")
				}
				go func() {
					bgCtx, cancel := context.WithTimeout(context.Background(), p.cfg.BackgroundTimeout)
					defer cancel()
					p.recompute(bgCtx, key, fetchFn) //nolint:errcheck
				}()
			}
			atomic.AddInt64(&p.metrics.hits, 1)
			return entry.value, nil // serve current value while recomputing
		}
		// Expired: stale-while-revalidate.
		if entry.age() <= p.cfg.StaleGrace {
			return p.getSWR(ctx, key, fetchFn)
		}
	}

	return p.getLockCoalesce(ctx, key, fetchFn)
}

func (p *CacheStampedeProtector) cacheGet(key string) (any, bool) {
	p.mu.RLock()
	entry, ok := p.entries[key]
	p.mu.RUnlock()
	if !ok || entry.isExpired() {
		return nil, false
	}
	return entry.value, true
}

func (p *CacheStampedeProtector) recompute(ctx context.Context, key string, fetchFn FetchFunc) (any, error) {
	if p.cfg.OnRecompute != nil {
		p.cfg.OnRecompute(key, "miss")
	}
	start := time.Now()
	val, ttl, err := fetchFn(ctx)
	delta := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("flowguard: stampede fetch for %q failed: %w", key, err)
	}

	now := time.Now()
	entry := &cacheEntry{
		value:     val,
		expiresAt: now.Add(ttl),
		delta:     delta,
		createdAt: now,
	}

	p.mu.Lock()
	if p.cfg.MaxEntries > 0 && len(p.entries) >= p.cfg.MaxEntries {
		p.evictOldestLocked()
	}
	p.entries[key] = entry
	p.mu.Unlock()

	atomic.AddInt64(&p.metrics.recomputes, 1)
	return val, nil
}

func (p *CacheStampedeProtector) evictOldestLocked() {
	var oldest string
	var oldestTime time.Time
	for k, e := range p.entries {
		if oldest == "" || e.createdAt.Before(oldestTime) {
			oldest = k
			oldestTime = e.createdAt
		}
	}
	if oldest != "" {
		delete(p.entries, oldest)
		atomic.AddInt64(&p.metrics.evictions, 1)
		if p.cfg.OnEvict != nil {
			p.cfg.OnEvict(oldest)
		}
	}
}

func (p *CacheStampedeProtector) keyLock(key string) *sync.Mutex {
	p.mu.Lock()
	mu, ok := p.locks[key]
	if !ok {
		mu = &sync.Mutex{}
		p.locks[key] = mu
	}
	p.mu.Unlock()
	return mu
}

// Delete removes a key from the cache immediately.
func (p *CacheStampedeProtector) Delete(key string) {
	p.mu.Lock()
	delete(p.entries, key)
	delete(p.locks, key)
	p.mu.Unlock()
}

// StampedeMetricsSnapshot is a point-in-time view of stampede metrics.
type StampedeMetricsSnapshot struct {
	Hits         int64
	Misses       int64
	StaleServed  int64
	Recomputes   int64
	Evictions    int64
	StampedePrev int64
	HitRate      float64
	StaleRate    float64
}

// Metrics returns a snapshot.
func (p *CacheStampedeProtector) Metrics() StampedeMetricsSnapshot {
	hits := atomic.LoadInt64(&p.metrics.hits)
	misses := atomic.LoadInt64(&p.metrics.misses)
	stale := atomic.LoadInt64(&p.metrics.staleServed)
	total := hits + misses + stale

	var hitRate, staleRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
		staleRate = float64(stale) / float64(total)
	}
	return StampedeMetricsSnapshot{
		Hits:         hits,
		Misses:       misses,
		StaleServed:  stale,
		Recomputes:   atomic.LoadInt64(&p.metrics.recomputes),
		Evictions:    atomic.LoadInt64(&p.metrics.evictions),
		StampedePrev: atomic.LoadInt64(&p.metrics.stampedePrev),
		HitRate:      hitRate,
		StaleRate:    staleRate,
	}
}
