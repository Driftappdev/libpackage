package registry

import "sync"

type Registry struct {
	mu    sync.RWMutex
	types map[string]func() any
}

func New() *Registry { return &Registry{types: map[string]func() any{}} }

func (r *Registry) Register(name string, factory func() any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[name] = factory
}

func (r *Registry) Resolve(name string) (func() any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.types[name]
	return v, ok
}
