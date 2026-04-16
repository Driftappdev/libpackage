package tx

import "context"

type Hook interface {
    BeforeBegin(context.Context, Options) error
    AfterBegin(context.Context, Options) error
    BeforeCommit(context.Context, Options) error
    AfterCommit(context.Context, Options) error
    BeforeRollback(context.Context, Options, error) error
    AfterRollback(context.Context, Options, error) error
}

type HookFunc struct {
    BeforeBeginFn    func(context.Context, Options) error
    AfterBeginFn     func(context.Context, Options) error
    BeforeCommitFn   func(context.Context, Options) error
    AfterCommitFn    func(context.Context, Options) error
    BeforeRollbackFn func(context.Context, Options, error) error
    AfterRollbackFn  func(context.Context, Options, error) error
}

func (f HookFunc) BeforeBegin(ctx context.Context, o Options) error { if f.BeforeBeginFn != nil { return f.BeforeBeginFn(ctx, o) }; return nil }
func (f HookFunc) AfterBegin(ctx context.Context, o Options) error { if f.AfterBeginFn != nil { return f.AfterBeginFn(ctx, o) }; return nil }
func (f HookFunc) BeforeCommit(ctx context.Context, o Options) error { if f.BeforeCommitFn != nil { return f.BeforeCommitFn(ctx, o) }; return nil }
func (f HookFunc) AfterCommit(ctx context.Context, o Options) error { if f.AfterCommitFn != nil { return f.AfterCommitFn(ctx, o) }; return nil }
func (f HookFunc) BeforeRollback(ctx context.Context, o Options, cause error) error { if f.BeforeRollbackFn != nil { return f.BeforeRollbackFn(ctx, o, cause) }; return nil }
func (f HookFunc) AfterRollback(ctx context.Context, o Options, cause error) error { if f.AfterRollbackFn != nil { return f.AfterRollbackFn(ctx, o, cause) }; return nil }
