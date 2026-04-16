package uow

import (
    "context"
    "sync"

    "github.com/driftappdev/libpackage/persistence/tx"
)

type UnitOfWork interface {
    Executor(context.Context) (tx.Executor, bool)
    Repository(context.Context, string) (any, error)
}

type DefaultUnitOfWork struct {
    registry *Registry
    cache    sync.Map
}

func New(registry *Registry) *DefaultUnitOfWork {
    if registry == nil { registry = NewRegistry() }
    return &DefaultUnitOfWork{registry: registry}
}

func (u *DefaultUnitOfWork) Executor(ctx context.Context) (tx.Executor, bool) { return tx.ExecutorFromContext(ctx) }
func (u *DefaultUnitOfWork) Repository(ctx context.Context, name string) (any, error) {
    if v, ok := u.cache.Load(name); ok { return v, nil }
    repo, err := u.registry.Resolve(ctx, u, name)
    if err != nil { return nil, err }
    if repo == nil { return nil, ErrNilRepository }
    u.cache.Store(name, repo)
    return repo, nil
}
