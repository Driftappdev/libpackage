package tx

import (
    "context"
    "database/sql"
)

type Beginner interface { BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) }

type Executor interface {
    ExecContext(context.Context, string, ...any) (sql.Result, error)
    QueryContext(context.Context, string, ...any) (*sql.Rows, error)
    QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Tx interface {
    Executor
    Commit() error
    Rollback() error
}

type Manager interface {
    Begin(context.Context, ...Option) (context.Context, Tx, error)
    InTx(context.Context, func(context.Context) error, ...Option) error
    Current(context.Context) (Executor, bool)
}

type SQLManager struct {
    db    Beginner
    hooks []Hook
}

func NewSQLManager(db Beginner, hooks ...Hook) *SQLManager { return &SQLManager{db: db, hooks: hooks} }
func (m *SQLManager) Current(ctx context.Context) (Executor, bool) { return ExecutorFromContext(ctx) }
