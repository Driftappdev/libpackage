package flowguard

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// RequestCoalescer merges concurrent identical requests into a single upstream call,
// distributing the result to all waiters. It extends the standard singleflight
// pattern with TTL-based caching, per-key metrics, timeout propagation, and
// panic recovery so a single bad request can't take down all waiters.
//
// Example:
//
//	c := NewRequestCoalescer(RequestCoalescerConfig{
//	    ResultTTL:    100 * time.Millisecond,
//	    MaxWaiters:   500,
//	    PanicRecover: true,
//	})
//	val, shared, err := c.Do(ctx, "user:42", func() (any, error) {
//	    return db.GetUser(ctx, 42)
//	})
type RequestCoalescer struct {
	cfg     RequestCoalescerConfig
	mu      sync.Mutex
	inflight map[string]*coalescedCall
	cache    map[string]*cachedResult
	metrics *coalescerMetrics
}

// RequestCoalescerConfig configures the RequestCoalescer.
type RequestCoalescerConfig struct {
	// ResultTTL is how long to cache a successful result so subsequent calls
	// within this window don't trigger a new upstream request.
	// 0 disables caching (pure coalescing of simultaneous requests only).
	ResultTTL time.Duration

	// ErrorTTL caches errors to prevent retry storms. 0 = don't cache errors.
	ErrorTTL time.Duration

	// MaxWaiters is the maximum number of goroutines that may queue behind a
	// single in-flight call. Extra callers receive ErrCoalescerFull immediately.
	MaxWaiters int

	// PanicRecover prevents a panic inside fn from propagating to all waiters.
	// The panic value is wrapped as an error.
	PanicRecover bool

	// ForgetOnError drops the in-flight call record on error so the next caller
	// retries immediately instead of getting the cached error.
	ForgetOnError bool

	// OnShare is called each time a caller joins an existing in-flight call.
	OnShare func(key string, waiters int)

	// OnCacheHit is called on a cache hit.
	OnCacheHit func(key string, age time.Duration)
}

func (c *RequestCoalescerConfig) setDefaults() {
	if c.MaxWaiters == 0 {
		c.MaxWaiters = 1000
	}
}

type coalescedCall struct {
	wg      sync.WaitGroup
	val     any
	err     error
	waiters int32
	done    chan struct{}
}

type cachedResult struct {
	val       any
	err       error
	expiresAt time.Time
}

func (r *cachedResult) isExpired() bool { return time.Now().After(r.expiresAt) }

type coalescerMetrics struct {
	requests    int64
	coalesced   int64
	cacheHits   int64
	errors      int64
	panics      int64
	fullDrops   int64
}

// CoalescerResult wraps the call result with sharing metadata.
type CoalescerResult struct {
	Value    any
	Shared   bool          // true if this call was coalesced with others
	CacheHit bool          // true if served from TTL cache
	Age      time.Duration // cache entry age (0 if not cached)
}

// NewRequestCoalescer creates a new RequestCoalescer.
func NewRequestCoalescer(cfg RequestCoalescerConfig) *RequestCoalescer {
	cfg.setDefaults()
	return &RequestCoalescer{
		cfg:      cfg,
		inflight: make(map[string]*coalescedCall),
		cache:    make(map[string]*cachedResult),
		metrics:  &coalescerMetrics{},
	}
}

// Do executes fn for the given key, coalescing concurrent identical calls.
// Returns (value, shared, error).
func (c *RequestCoalescer) Do(ctx context.Context, key string, fn func() (any, error)) (*CoalescerResult, error) {
	atomic.AddInt64(&c.metrics.requests, 1)

	// Fast path: check cache.
	if r := c.cacheGet(key); r != nil {
		return r, nil
	}

	c.mu.Lock()

	// Recheck cache under lock.
	if cached, ok := c.cache[key]; ok && !cached.isExpired() {
		c.mu.Unlock()
		age := time.Since(cached.expiresAt.Add(-c.cfg.ResultTTL))
		atomic.AddInt64(&c.metrics.cacheHits, 1)
		if c.cfg.OnCacheHit != nil {
			c.cfg.OnCacheHit(key, age)
		}
		return &CoalescerResult{Value: cached.val, CacheHit: true, Age: age}, cached.err
	}

	// Join existing in-flight call.
	if call, ok := c.inflight[key]; ok {
		waiters := atomic.AddInt32(&call.waiters, 1)
		if int(waiters) > c.cfg.MaxWaiters {
			atomic.AddInt32(&call.waiters, -1)
			c.mu.Unlock()
			atomic.AddInt64(&c.metrics.fullDrops, 1)
			return nil, ErrCoalescerFull
		}
		atomic.AddInt64(&c.metrics.coalesced, 1)
		if c.cfg.OnShare != nil {
			c.cfg.OnShare(key, int(waiters))
		}
		c.mu.Unlock()
		return c.wait(ctx, call, key, true)
	}

	// This goroutine is the leader.
	call := &coalescedCall{done: make(chan struct{})}
	call.wg.Add(1)
	c.inflight[key] = call
	c.mu.Unlock()

	// Execute the function.
	go c.execute(key, call, fn)
	return c.wait(ctx, call, key, false)
}

func (c *RequestCoalescer) execute(key string, call *coalescedCall, fn func() (any, error)) {
	defer func() {
		if p := recover(); p != nil {
			atomic.AddInt64(&c.metrics.panics, 1)
			if c.cfg.PanicRecover {
				call.err = fmt.Errorf("flowguard: coalescer panic for key %q: %v", key, p)
			} else {
				panic(p)
			}
		}

		c.mu.Lock()
		if call.err == nil && c.cfg.ResultTTL > 0 {
			c.cache[key] = &cachedResult{
				val:       call.val,
				expiresAt: time.Now().Add(c.cfg.ResultTTL),
			}
		} else if call.err != nil && c.cfg.ErrorTTL > 0 && !c.cfg.ForgetOnError {
			c.cache[key] = &cachedResult{
				err:       call.err,
				expiresAt: time.Now().Add(c.cfg.ErrorTTL),
			}
		}
		delete(c.inflight, key)
		c.mu.Unlock()

		close(call.done)
		call.wg.Done()
	}()

	call.val, call.err = fn()
	if call.err != nil {
		atomic.AddInt64(&c.metrics.errors, 1)
	}
}

func (c *RequestCoalescer) wait(ctx context.Context, call *coalescedCall, key string, shared bool) (*CoalescerResult, error) {
	select {
	case <-call.done:
		return &CoalescerResult{Value: call.val, Shared: shared}, call.err
	case <-ctx.Done():
		return nil, fmt.Errorf("flowguard: coalescer wait for key %q canceled: %w", key, ctx.Err())
	}
}

func (c *RequestCoalescer) cacheGet(key string) *CoalescerResult {
	c.mu.Lock()
	cached, ok := c.cache[key]
	if ok && cached.isExpired() {
		delete(c.cache, key)
		ok = false
	}
	c.mu.Unlock()

	if !ok {
		return nil
	}
	age := time.Since(cached.expiresAt.Add(-c.cfg.ResultTTL))
	atomic.AddInt64(&c.metrics.cacheHits, 1)
	return &CoalescerResult{Value: cached.val, CacheHit: true, Age: age, Shared: true}
}

// Forget removes a key from both the in-flight map and cache.
// Useful for forcing re-fetch on next call.
func (c *RequestCoalescer) Forget(key string) {
	c.mu.Lock()
	delete(c.inflight, key)
	delete(c.cache, key)
	c.mu.Unlock()
}

// ForgetAll clears all cached results and cancels all tracking.
func (c *RequestCoalescer) ForgetAll() {
	c.mu.Lock()
	c.cache = make(map[string]*cachedResult)
	c.mu.Unlock()
}

// PurgeExpired removes all expired cache entries. Should be called periodically
// by the owner (e.g. via a background goroutine) to prevent unbounded growth.
func (c *RequestCoalescer) PurgeExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k, v := range c.cache {
		if v.isExpired() {
			delete(c.cache, k)
			n++
		}
	}
	return n
}

// CoalescerMetricsSnapshot is a point-in-time snapshot of coalescer metrics.
type CoalescerMetricsSnapshot struct {
	Requests   int64
	Coalesced  int64
	CacheHits  int64
	Errors     int64
	Panics     int64
	FullDrops  int64
	CoalesceRate float64
	CacheHitRate float64
}

// Metrics returns a snapshot of coalescer metrics.
func (c *RequestCoalescer) Metrics() CoalescerMetricsSnapshot {
	req := atomic.LoadInt64(&c.metrics.requests)
	coal := atomic.LoadInt64(&c.metrics.coalesced)
	hits := atomic.LoadInt64(&c.metrics.cacheHits)

	var coalRate, hitRate float64
	if req > 0 {
		coalRate = float64(coal) / float64(req)
		hitRate = float64(hits) / float64(req)
	}
	return CoalescerMetricsSnapshot{
		Requests:     req,
		Coalesced:    coal,
		CacheHits:    hits,
		Errors:       atomic.LoadInt64(&c.metrics.errors),
		Panics:       atomic.LoadInt64(&c.metrics.panics),
		FullDrops:    atomic.LoadInt64(&c.metrics.fullDrops),
		CoalesceRate: coalRate,
		CacheHitRate: hitRate,
	}
}

// ErrCoalescerFull is returned when MaxWaiters is exceeded for a given key.
var ErrCoalescerFull = fmt.Errorf("flowguard: coalescer waiter queue full")
