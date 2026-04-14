package gocontainer

import (
	"fmt"
	"sync"
)

type Provider func(c *Container) (any, error)

type Container struct {
	mu        sync.RWMutex
	providers map[string]Provider
	instances map[string]any
}

func New() *Container {
	return &Container{providers: map[string]Provider{}, instances: map[string]any{}}
}
func (c *Container) Register(name string, p Provider) { c.providers[name] = p }
func (c *Container) Resolve(name string) (any, error) {
	if inst, ok := c.instances[name]; ok {
		return inst, nil
	}
	p, ok := c.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	inst, err := p(c)
	if err != nil {
		return nil, err
	}
	c.instances[name] = inst
	return inst, nil
}
