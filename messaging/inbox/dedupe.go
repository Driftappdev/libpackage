package inbox

import (
    "context"
    "sync"
    "time"
)

type DedupeStore interface {
    Seen(context.Context, string) (bool, error)
    Mark(context.Context, string, time.Time) error
}

type MemoryDedupe struct {
    mu sync.Mutex
    m  map[string]time.Time
}

func NewMemoryDedupe() *MemoryDedupe { return &MemoryDedupe{m: make(map[string]time.Time)} }
func (d *MemoryDedupe) Seen(_ context.Context, key string) (bool, error) { d.mu.Lock(); defer d.mu.Unlock(); _, ok := d.m[key]; return ok, nil }
func (d *MemoryDedupe) Mark(_ context.Context, key string, at time.Time) error { d.mu.Lock(); defer d.mu.Unlock(); d.m[key] = at; return nil }
