package registry

import "sync"

type Plugin interface{ Name() string }

type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

func New() *Registry { return &Registry{plugins: map[string]Plugin{}} }

func (r *Registry) Register(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
}

func (r *Registry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// GenericRegistry is a reusable typed map-based registry.
type GenericRegistry[T any] struct {
	mu    sync.RWMutex
	items map[string]T
}

func NewGeneric[T any]() *GenericRegistry[T] {
	return &GenericRegistry[T]{items: map[string]T{}}
}

func (r *GenericRegistry[T]) Set(k string, v T) {
	r.mu.Lock()
	r.items[k] = v
	r.mu.Unlock()
}

func (r *GenericRegistry[T]) GetTyped(k string) (T, bool) {
	r.mu.RLock()
	v, ok := r.items[k]
	r.mu.RUnlock()
	return v, ok
}
