// Package gocache provides a production-grade in-process cache with TTL,
// LRU/LFU/FIFO eviction, generic API, stats, and singleflight stampede prevention.
package gocache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNotFound = errors.New("gocache: key not found")

type EvictionPolicy int

const (
	PolicyLRU EvictionPolicy = iota
	PolicyLFU
	PolicyFIFO
)

type item[V any] struct {
	value     V
	expiresAt time.Time
	freq      uint64
	accessAt  time.Time
	key       string
}

func (i *item[V]) expired() bool {
	return !i.expiresAt.IsZero() && time.Now().After(i.expiresAt)
}

type Options struct {
	MaxSize         int
	DefaultTTL      time.Duration
	Policy          EvictionPolicy
	OnEvict         func(key string, reason string)
	CleanupInterval time.Duration
}

type Stats struct {
	Hits      uint64
	Misses    uint64
	Sets      uint64
	Deletes   uint64
	Evictions uint64
	Size      int
}

func (s Stats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total) * 100
}

type Cache[K comparable, V any] struct {
	mu      sync.RWMutex
	items   map[K]*item[V]
	order   []K
	opts    Options
	hits    atomic.Uint64
	misses  atomic.Uint64
	sets    atomic.Uint64
	deletes atomic.Uint64
	evicts  atomic.Uint64
	stop    chan struct{}
}

func New[K comparable, V any](opts Options) *Cache[K, V] {
	if opts.CleanupInterval == 0 {
		opts.CleanupInterval = time.Minute
	}
	c := &Cache[K, V]{
		items: make(map[K]*item[V]),
		opts:  opts,
		stop:  make(chan struct{}),
	}
	go c.janitor()
	return c
}

func (c *Cache[K, V]) Set(key K, value V, ttl time.Duration) {
	if ttl == 0 {
		ttl = c.opts.DefaultTTL
	}
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	strKey := fmt.Sprint(key)
	if _, exists := c.items[key]; !exists {
		if c.opts.MaxSize > 0 && len(c.items) >= c.opts.MaxSize {
			c.evict()
		}
		c.order = append(c.order, key)
	}
	c.items[key] = &item[V]{value: value, expiresAt: exp, accessAt: time.Now(), key: strKey}
	c.sets.Add(1)
}

func (c *Cache[K, V]) Get(key K) (V, error) {
	c.mu.Lock()
	it, ok := c.items[key]
	if !ok || it.expired() {
		if ok {
			delete(c.items, key)
			c.removeOrder(key)
		}
		c.mu.Unlock()
		c.misses.Add(1)
		var zero V
		return zero, ErrNotFound
	}
	it.freq++
	it.accessAt = time.Now()
	v := it.value
	c.mu.Unlock()
	c.hits.Add(1)
	return v, nil
}

func (c *Cache[K, V]) GetOrSet(key K, ttl time.Duration, loader func() (V, error)) (V, error) {
	if v, err := c.Get(key); err == nil {
		return v, nil
	}
	v, err := loader()
	if err != nil {
		return v, err
	}
	c.Set(key, v, ttl)
	return v, nil
}

func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		delete(c.items, key)
		c.removeOrder(key)
		c.deletes.Add(1)
	}
}

func (c *Cache[K, V]) Flush() {
	c.mu.Lock()
	c.items = make(map[K]*item[V])
	c.order = nil
	c.mu.Unlock()
}

func (c *Cache[K, V]) Has(key K) bool {
	c.mu.RLock()
	it, ok := c.items[key]
	c.mu.RUnlock()
	return ok && !it.expired()
}

func (c *Cache[K, V]) Keys() []K {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]K, 0, len(c.items))
	for k, it := range c.items {
		if !it.expired() {
			out = append(out, k)
		}
	}
	return out
}

func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n := 0
	for _, it := range c.items {
		if !it.expired() {
			n++
		}
	}
	return n
}

func (c *Cache[K, V]) Stats() Stats {
	c.mu.RLock()
	size := len(c.items)
	c.mu.RUnlock()
	return Stats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Sets:      c.sets.Load(),
		Deletes:   c.deletes.Load(),
		Evictions: c.evicts.Load(),
		Size:      size,
	}
}

func (c *Cache[K, V]) Close() { close(c.stop) }

func (c *Cache[K, V]) janitor() {
	t := time.NewTicker(c.opts.CleanupInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.deleteExpired()
		case <-c.stop:
			return
		}
	}
}

func (c *Cache[K, V]) deleteExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, it := range c.items {
		if it.expired() {
			delete(c.items, k)
			c.removeOrder(k)
			c.evicts.Add(1)
			if c.opts.OnEvict != nil {
				c.opts.OnEvict(it.key, "expired")
			}
		}
	}
}

func (c *Cache[K, V]) evict() {
	if len(c.order) == 0 {
		return
	}
	switch c.opts.Policy {
	case PolicyLFU:
		var minKey K
		var minFreq uint64 = ^uint64(0)
		for k, it := range c.items {
			if it.freq < minFreq {
				minFreq = it.freq
				minKey = k
			}
		}
		it := c.items[minKey]
		delete(c.items, minKey)
		c.removeOrder(minKey)
		c.evicts.Add(1)
		if c.opts.OnEvict != nil {
			c.opts.OnEvict(it.key, "lfu")
		}
	case PolicyFIFO:
		oldest := c.order[0]
		c.order = c.order[1:]
		if it, ok := c.items[oldest]; ok {
			delete(c.items, oldest)
			c.evicts.Add(1)
			if c.opts.OnEvict != nil {
				c.opts.OnEvict(it.key, "fifo")
			}
		}
	default:
		var lruKey K
		lruAccess := time.Now().Add(time.Hour)
		for k, it := range c.items {
			if it.accessAt.Before(lruAccess) {
				lruAccess = it.accessAt
				lruKey = k
			}
		}
		it := c.items[lruKey]
		delete(c.items, lruKey)
		c.removeOrder(lruKey)
		c.evicts.Add(1)
		if c.opts.OnEvict != nil {
			c.opts.OnEvict(it.key, "lru")
		}
	}
}

func (c *Cache[K, V]) removeOrder(key K) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// Group prevents cache stampedes via singleflight coalescing.
type Group[K comparable, V any] struct {
	mu    sync.Mutex
	calls map[K]*call[V]
	cache *Cache[K, V]
}

type call[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

func NewGroup[K comparable, V any](cache *Cache[K, V]) *Group[K, V] {
	return &Group[K, V]{calls: make(map[K]*call[V]), cache: cache}
}

func (g *Group[K, V]) Do(ctx context.Context, key K, ttl time.Duration, fn func(ctx context.Context) (V, error)) (V, error) {
	if v, err := g.cache.Get(key); err == nil {
		return v, nil
	}
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &call[V]{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	c.val, c.err = fn(ctx)
	if c.err == nil {
		g.cache.Set(key, c.val, ttl)
	}
	c.wg.Done()

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	return c.val, c.err
}
