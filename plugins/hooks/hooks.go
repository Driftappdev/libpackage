package hooks

import "context"

// Hook is the shared lifecycle hook contract used by plugin orchestration.
type Hook interface {
	Name() string
	BeforeInit(context.Context) error
	AfterInit(context.Context) error
	BeforeShutdown(context.Context) error
	AfterShutdown(context.Context) error
}

// StartStopHook keeps backward-compat shape for callers that only need
// start/stop hooks.
type StartStopHook interface {
	BeforeStart(ctx context.Context) error
	AfterStart(ctx context.Context) error
	BeforeStop(ctx context.Context) error
}

// Nop is a no-op Hook implementation.
type Nop struct {
	HookName string
}

func (n Nop) Name() string                         { return n.HookName }
func (n Nop) BeforeInit(context.Context) error     { return nil }
func (n Nop) AfterInit(context.Context) error      { return nil }
func (n Nop) BeforeShutdown(context.Context) error { return nil }
func (n Nop) AfterShutdown(context.Context) error  { return nil }

// StartStopAdapter adapts legacy StartStopHook into the richer Hook lifecycle.
type StartStopAdapter struct {
	HookName string
	Legacy   StartStopHook
}

func (a StartStopAdapter) Name() string {
	return a.HookName
}

func (a StartStopAdapter) BeforeInit(ctx context.Context) error {
	if a.Legacy == nil {
		return nil
	}
	return a.Legacy.BeforeStart(ctx)
}

func (a StartStopAdapter) AfterInit(ctx context.Context) error {
	if a.Legacy == nil {
		return nil
	}
	return a.Legacy.AfterStart(ctx)
}

func (a StartStopAdapter) BeforeShutdown(ctx context.Context) error {
	if a.Legacy == nil {
		return nil
	}
	return a.Legacy.BeforeStop(ctx)
}

func (a StartStopAdapter) AfterShutdown(context.Context) error {
	return nil
}

// HookFunc keeps compatibility with function-style hooks used by platform.
type HookFunc func(context.Context) error

// Set groups before/after HookFunc callbacks.
type Set struct {
	before []HookFunc
	after  []HookFunc
}

func (s *Set) Before(h HookFunc) { s.before = append(s.before, h) }
func (s *Set) After(h HookFunc)  { s.after = append(s.after, h) }

func (s *Set) RunBefore(ctx context.Context) error {
	for _, h := range s.before {
		if err := h(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Set) RunAfter(ctx context.Context) error {
	for _, h := range s.after {
		if err := h(ctx); err != nil {
			return err
		}
	}
	return nil
}
