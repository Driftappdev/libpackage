package uow

import (
    "context"

    "github.com/driftappdev/libpackage/persistence/tx"
)

type Coordinator struct {
    txm tx.Manager
    reg *Registry
}

func NewCoordinator(txm tx.Manager, reg *Registry) *Coordinator {
    if reg == nil { reg = NewRegistry() }
    return &Coordinator{txm: txm, reg: reg}
}

func (c *Coordinator) Do(ctx context.Context, fn func(context.Context, UnitOfWork) error, opts ...tx.Option) error {
    if c.txm == nil {
        u := New(c.reg)
        return fn(WithContext(ctx, u), u)
    }
    return c.txm.InTx(ctx, func(txCtx context.Context) error {
        u := New(c.reg)
        txCtx = WithContext(txCtx, u)
        return fn(txCtx, u)
    }, opts...)
}
