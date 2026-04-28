package shutdown

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Hook func(context.Context) error

type NamedHook struct {
	Name string
	Fn   Hook
}

func (h NamedHook) Run(ctx context.Context) error {
	if h.Fn == nil {
		return nil
	}
	return h.Fn(ctx)
}

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

func NotifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

func Server(parent context.Context, server *http.Server, timeout time.Duration) error {
	ctx, cancel := WithTimeout(parent, timeout)
	defer cancel()
	return server.Shutdown(ctx)
}

func Signals(sig ...os.Signal) <-chan os.Signal {
	if len(sig) == 0 {
		sig = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig...)
	return ch
}
