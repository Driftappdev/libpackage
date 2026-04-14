package shutdown

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Hook func(context.Context) error

type Manager struct {
	mu      sync.Mutex
	hooks   []Hook
	timeout time.Duration
}

func NewManager(timeout time.Duration) *Manager { return &Manager{timeout: timeout} }
func (m *Manager) Add(h Hook)                   { m.mu.Lock(); defer m.mu.Unlock(); m.hooks = append(m.hooks, h) }

func (m *Manager) Run(ctx context.Context) error {
	m.mu.Lock()
	hooks := append([]Hook(nil), m.hooks...)
	m.mu.Unlock()
	shutdownCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	var errs []error
	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i](shutdownCtx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
