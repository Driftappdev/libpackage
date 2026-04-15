package goregistry

import "sync"

type Registry[T any] struct {
	mu    sync.RWMutex
	items map[string]T
}

func New[T any]() *Registry[T]           { return &Registry[T]{items: map[string]T{}} }
func (r *Registry[T]) Set(k string, v T) { r.mu.Lock(); r.items[k] = v; r.mu.Unlock() }
func (r *Registry[T]) Get(k string) (T, bool) {
	r.mu.RLock()
	v, ok := r.items[k]
	r.mu.RUnlock()
	return v, ok
}
