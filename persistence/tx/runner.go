package tx

import (
    "context"
    "database/sql"
    "errors"
)

func (m *SQLManager) Begin(ctx context.Context, opts ...Option) (context.Context, Tx, error) {
    if _, ok := ExecutorFromContext(ctx); ok { return ctx, nil, ErrAlreadyInTx }
    o := BuildOptions(opts...)
    for _, h := range m.hooks { if err := h.BeforeBegin(ctx, o); err != nil { return ctx, nil, err } }
    ctx, cancel := applyTimeout(ctx, o)
    iso, err := o.Isolation.SQL()
    if err != nil { cancel(); return ctx, nil, err }
    tx, err := m.db.BeginTx(ctx, &sql.TxOptions{Isolation: iso, ReadOnly: o.ReadOnly})
    if err != nil { cancel(); return ctx, nil, err }
    ctx = WithExecutor(ctx, tx, Meta{Name: o.Name, ReadOnly: o.ReadOnly, Isolation: o.Isolation})
    for _, h := range m.hooks {
        if err := h.AfterBegin(ctx, o); err != nil { _ = tx.Rollback(); cancel(); return ctx, nil, err }
    }
    return context.WithValue(ctx, contextKey("tx.cancel"), cancel), tx, nil
}

func (m *SQLManager) InTx(ctx context.Context, fn func(context.Context) error, opts ...Option) (err error) {
    if fn == nil { return nil }
    o := BuildOptions(opts...)
    if existing, ok := ExecutorFromContext(ctx); ok {
        switch o.Propagation {
        case PropagationNever:
            return ErrAlreadyInTx
        case PropagationSupports, PropagationRequired, PropagationMandatory:
            _ = existing
            return fn(ctx)
        case PropagationRequiresNew:
        }
    } else if o.Propagation == PropagationMandatory {
        return ErrNoActiveTx
    } else if o.Propagation == PropagationSupports || o.Propagation == PropagationNever {
        return fn(ctx)
    }
    txCtx, tx, err := m.Begin(ctx, opts...)
    if err != nil { return err }
    var cause error
    defer func() {
        if c, ok := txCtx.Value(contextKey("tx.cancel")).(context.CancelFunc); ok { defer c() }
        if r := recover(); r != nil {
            if o.RollbackOnPanic { cause = errors.New("panic rollback"); _ = rollback(txCtx, tx, m.hooks, o, cause) }
            panic(r)
        }
        if err != nil { cause = err; _ = rollback(txCtx, tx, m.hooks, o, cause); return }
        err = commit(txCtx, tx, m.hooks, o)
    }()
    err = fn(txCtx)
    return err
}

func commit(ctx context.Context, tx Tx, hooks []Hook, o Options) error {
    for _, h := range hooks { if err := h.BeforeCommit(ctx, o); err != nil { return err } }
    if err := tx.Commit(); err != nil { return err }
    for _, h := range hooks { if err := h.AfterCommit(ctx, o); err != nil { return err } }
    return nil
}

func rollback(ctx context.Context, tx Tx, hooks []Hook, o Options, cause error) error {
    for _, h := range hooks { if err := h.BeforeRollback(ctx, o, cause); err != nil { return err } }
    if err := tx.Rollback(); err != nil { return err }
    for _, h := range hooks { if err := h.AfterRollback(ctx, o, cause); err != nil { return err } }
    return nil
}
