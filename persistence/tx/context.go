package tx

import "context"

type contextKey string

const (
    txContextKey   contextKey = "tx.executor"
    metaContextKey contextKey = "tx.meta"
)

type Meta struct {
    Name         string
    ReadOnly     bool
    Isolation    IsolationLevel
    RollbackOnly bool
}

func WithExecutor(ctx context.Context, exec Executor, meta Meta) context.Context {
    ctx = context.WithValue(ctx, txContextKey, exec)
    return context.WithValue(ctx, metaContextKey, meta)
}

func ExecutorFromContext(ctx context.Context) (Executor, bool) {
    v, ok := ctx.Value(txContextKey).(Executor)
    return v, ok
}

func MetaFromContext(ctx context.Context) (Meta, bool) {
    v, ok := ctx.Value(metaContextKey).(Meta)
    return v, ok
}
