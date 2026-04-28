package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Component interface {
	Start(context.Context) error
	Stop(context.Context) error
	Name() string
}

type App struct{ components []Component }

func New(components ...Component) *App { return &App{components: components} }

func (a *App) Start(ctx context.Context) error {
	for _, c := range a.components {
		if err := c.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	var errs []error
	for i := len(a.components) - 1; i >= 0; i-- {
		if err := a.components[i].Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Manager is a thin orchestration wrapper around App.
type Manager struct {
	app *App
}

func NewManager(components ...Component) *Manager {
	return &Manager{app: New(components...)}
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil || m.app == nil {
		return nil
	}
	return m.app.Start(ctx)
}

func (m *Manager) Stop(ctx context.Context) error {
	if m == nil || m.app == nil {
		return nil
	}
	return m.app.Stop(ctx)
}

type Hook struct {
	OnStart func(context.Context) error
	OnStop  func(context.Context) error
}

type hookRegistration struct {
	name string
	hook Hook
}

// HookManager is a named lifecycle orchestration manager.
type HookManager struct {
	mu      sync.Mutex
	items   []hookRegistration
	started bool
}

func NewHookManager() *HookManager { return &HookManager{} }

func (m *HookManager) Register(name string, hook Hook) error {
	if name == "" {
		return errors.New("lifecycle: name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("lifecycle: cannot register after start")
	}
	m.items = append(m.items, hookRegistration{name: name, hook: hook})
	return nil
}

func (m *HookManager) MustRegister(name string, hook Hook) {
	if err := m.Register(name, hook); err != nil {
		panic(err)
	}
}

func (m *HookManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return errors.New("lifecycle: already started")
	}
	items := append([]hookRegistration(nil), m.items...)
	m.started = true
	m.mu.Unlock()

	started := make([]hookRegistration, 0, len(items))
	for _, item := range items {
		if item.hook.OnStart == nil {
			started = append(started, item)
			continue
		}
		if err := item.hook.OnStart(ctx); err != nil {
			_ = stopHooksReverse(ctx, started)
			return fmt.Errorf("lifecycle: start %s: %w", item.name, err)
		}
		started = append(started, item)
	}
	return nil
}

func (m *HookManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	items := append([]hookRegistration(nil), m.items...)
	m.started = false
	m.mu.Unlock()
	return stopHooksReverse(ctx, items)
}

func stopHooksReverse(ctx context.Context, items []hookRegistration) error {
	var errs []error
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].hook.OnStop == nil {
			continue
		}
		if err := items[i].hook.OnStop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", items[i].name, err))
		}
	}
	return errors.Join(errs...)
}
