package lifecycle

import "context"

type Component interface {
	Start(context.Context) error
	Stop(context.Context) error
	Name() string
}
