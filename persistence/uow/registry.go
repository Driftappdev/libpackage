package uow

import (
    "context"
    "sync"
)

type Registry struct {
    mu        sync.RWMutex
    factories map[string]Factory
}

func NewRegistry() *Registry { return &Registry{factories: make(map[string]Factory)} }

func (r *Registry) Register(name string, factory Factory) error {
    r.mu.Lock(); defer r.mu.Unlock()
    if _, exists := r.factories[name]; exists { return ErrDuplicateFactory }
    r.factories[name] = factory
    return nil
}

func (r *Registry) Resolve(ctx context.Context, u UnitOfWork, name string) (any, error) {
    r.mu.RLock(); f, ok := r.factories[name]; r.mu.RUnlock()
    if !ok { return nil, ErrFactoryNotFound }
    return f(ctx, u)
}
