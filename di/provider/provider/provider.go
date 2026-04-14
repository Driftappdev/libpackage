package provider

import "context"

type Provider interface {
	Name() string
	Build() (any, error)
}

// Factory/Definition provide compatibility with hook-driven DI flows.
type Factory func(context.Context) (any, error)

type Definition struct {
	Name      string
	Factory   Factory
	Singleton bool
}
