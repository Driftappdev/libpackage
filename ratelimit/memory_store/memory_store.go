package ratelimit

import (
	"context"
	"sync"
	"time"
)

type memoryItem struct {
	count   int64
	resetAt time.Time
}

type MemoryStore struct {
	mu    sync.Mutex
	items map[string]memoryItem
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{items: map[string]memoryItem{}} }

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
