package uow

import "context"

type contextKey string

const unitContextKey contextKey = "uow.unit"

func WithContext(ctx context.Context, u UnitOfWork) context.Context { return context.WithValue(ctx, unitContextKey, u) }
func FromContext(ctx context.Context) (UnitOfWork, bool) { u, ok := ctx.Value(unitContextKey).(UnitOfWork); return u, ok }
