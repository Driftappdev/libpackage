package goloader

import "context"

type Module interface {
	Name() string
	Load(ctx context.Context) error
	Unload(ctx context.Context) error
}
