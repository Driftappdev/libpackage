package uow

import "context"

type Factory func(context.Context, UnitOfWork) (any, error)
