package container

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/platformcore/libpackage/orchestration/di/registry"
)

type Factory func(c *Container) (any, error)

type Container struct {
	mu         sync.RWMutex
	factories  map[string]Factory
	singletons map[string]any
	registry   *registry.Registry
}

func New() *Container {
	return &Container{factories: map[string]Factory{}, singletons: map[string]any{}}
}

func NewWithRegistry(reg *registry.Registry) *Container {
	if reg == nil {
		reg = registry.New()
	}
	return &Container{
		factories:  map[string]Factory{},
		singletons: map[string]any{},
		registry:   reg,
	}
}

func (c *Container) Register(name string, factory Factory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.factories[name] = factory
}

func (c *Container) Resolve(name string) (any, error) {
	c.mu.RLock()
	if v, ok := c.singletons[name]; ok {
		c.mu.RUnlock()
		return v, nil
	}
	factory, ok := c.factories[name]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.singletons[name]; ok {
		return v, nil
	}
	v, err := factory(c)
	if err != nil {
		return nil, err
	}
	c.singletons[name] = v
	return v, nil
}

func (c *Container) ResolveWithContext(ctx context.Context, name string) (any, error) {
	if c.registry == nil {
		return nil, errors.New("container: registry is nil")
	}
	def, ok := c.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("container: provider not found: %s", name)
	}
	if !def.Singleton {
		return def.Factory(ctx)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.singletons[name]; ok {
		return v, nil
	}
	v, err := def.Factory(ctx)
	if err != nil {
		return nil, err
	}
	c.singletons[name] = v
	return v, nil
}

func MustResolve[T any](ctx context.Context, c *Container, name string) T {
	v, err := c.ResolveWithContext(ctx, name)
	if err != nil {
		panic(err)
	}
	out, ok := v.(T)
	if !ok {
		panic(errors.New("container: invalid type assertion"))
	}
	return out
}


