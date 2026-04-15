package ratelimit

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

var (
	ErrBadWindow       = errors.New("invalid window")
	ErrBadLimit        = errors.New("invalid limit")
	ErrBadCost         = errors.New("invalid cost")
	ErrBadBurst        = errors.New("invalid burst")
	ErrUnsupportedMode = errors.New("unsupported strategy")
)

type Strategy string

const (
	StrategyFixedWindow   Strategy = "fixed_window"
	StrategySlidingWindow Strategy = "sliding_window"
	StrategyTokenBucket   Strategy = "token_bucket"
)

type Policy struct {
	Name                string
	Limit               int64
	Window              time.Duration
	Strategy            Strategy
	Cost                int64
	Burst               int64
	RefillRatePerSecond float64
}

func (p Policy) Normalize() Policy {
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Window <= 0 {
		p.Window = time.Minute
	}
	if p.Strategy == "" {
		p.Strategy = StrategyFixedWindow
	}
	if p.Cost <= 0 {
		p.Cost = 1
	}
	if p.Burst <= 0 {
		p.Burst = p.Limit
	}
	if p.RefillRatePerSecond <= 0 {
		p.RefillRatePerSecond = float64(p.Limit) / p.Window.Seconds()
	}
	return p
}

type memoryItem struct {
	count      int64
	resetAt    time.Time
	hits       []time.Time
	tokens     float64
	lastRefill time.Time
}

type MemoryStore struct {
	mu    sync.Mutex
	items map[string]memoryItem
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{items: map[string]memoryItem{}} }

type StoreRequest struct {
	Key    string
	Policy Policy
	Now    time.Time
}

type StoreResponse struct {
	Used      int64
	Limit     int64
	Remaining int64
	ResetAt   time.Time
	Allowed   bool
	Metadata  map[string]string
}

func (s *MemoryStore) Increment(_ context.Context, key string, window time.Duration, now time.Time) (int64, time.Time, error) {
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
	remaining := max64(0, p.Limit-used)
	return StoreResponse{
		Used:      used,
		Limit:     p.Limit,
		Remaining: remaining,
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
	for i := int64(0); i < p.Cost; i++ {
		item.hits = append(item.hits, now)
	}
	s.items[key] = item

	used := int64(len(item.hits))
	remaining := max64(0, p.Limit-used)
	return StoreResponse{
		Used:      used,
		Limit:     p.Limit,
		Remaining: remaining,
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
	remaining := int64(item.tokens)
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

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
