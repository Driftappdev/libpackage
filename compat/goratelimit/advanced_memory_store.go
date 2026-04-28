package goratelimit

import (
	"context"
	"math"
	"sync"
	"time"
)

type memoryItem struct {
	count      int
	resetAt    time.Time
	hits       []time.Time
	tokens     float64
	lastRefill time.Time
}

// MemoryStore is a pluggable in-memory store compatible with Engine.
type MemoryStore struct {
	mu    sync.Mutex
	items map[string]memoryItem
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{items: map[string]memoryItem{}} }

func (s *MemoryStore) Increment(_ context.Context, key string, window time.Duration, now time.Time) (int, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok || now.After(item.resetAt) {
		item = memoryItem{resetAt: now.Add(window)}
	}
	item.count++
	s.items[key] = item
	return item.count, item.resetAt, nil
}

func (s *MemoryStore) Eval(_ context.Context, req StoreRequest) (StoreResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := req.Policy.Normalize()
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	item := s.items[req.Key]
	switch p.Strategy {
	case StrategyTokenBucket:
		return s.evalTokenBucket(req.Key, item, p, now), nil
	case StrategySlidingWindow:
		return s.evalSliding(req.Key, item, p, now), nil
	default:
		return s.evalFixed(req.Key, item, p, now), nil
	}
}

func (s *MemoryStore) evalFixed(key string, item memoryItem, p Policy, now time.Time) StoreResponse {
	if now.After(item.resetAt) || item.resetAt.IsZero() {
		item = memoryItem{resetAt: now.Add(p.Window)}
	}
	item.count += p.Cost
	s.items[key] = item

	used := item.count
	return StoreResponse{
		Used:      used,
		Limit:     p.Limit,
		Remaining: maxInt(0, p.Limit-used),
		ResetAt:   item.resetAt,
		Allowed:   used <= p.Limit,
		Metadata:  map[string]string{"mode": string(StrategyFixedWindow)},
	}
}

func (s *MemoryStore) evalSliding(key string, item memoryItem, p Policy, now time.Time) StoreResponse {
	cutoff := now.Add(-p.Window)
	kept := item.hits[:0]
	for _, t := range item.hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	item.hits = kept
	for i := 0; i < p.Cost; i++ {
		item.hits = append(item.hits, now)
	}
	s.items[key] = item

	used := len(item.hits)
	return StoreResponse{
		Used:      used,
		Limit:     p.Limit,
		Remaining: maxInt(0, p.Limit-used),
		ResetAt:   now.Add(p.Window),
		Allowed:   used <= p.Limit,
		Metadata:  map[string]string{"mode": string(StrategySlidingWindow)},
	}
}

func (s *MemoryStore) evalTokenBucket(key string, item memoryItem, p Policy, now time.Time) StoreResponse {
	if item.tokens == 0 && item.lastRefill.IsZero() {
		item.tokens = float64(p.Burst)
		item.lastRefill = now
	}
	elapsed := now.Sub(item.lastRefill).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	item.tokens = math.Min(float64(p.Burst), item.tokens+(elapsed*p.RefillRatePerSecond))
	item.lastRefill = now

	allowed := item.tokens >= float64(p.Cost)
	if allowed {
		item.tokens -= float64(p.Cost)
	}
	s.items[key] = item
	remaining := int(item.tokens)
	if remaining < 0 {
		remaining = 0
	}
	return StoreResponse{
		Used:      p.Burst - remaining,
		Limit:     p.Burst,
		Remaining: remaining,
		ResetAt:   now.Add(time.Second),
		Allowed:   allowed,
		Metadata:  map[string]string{"mode": string(StrategyTokenBucket)},
	}
}
