// Package registry provides a centralised, thread-safe service registry for
// managing named middleware chains, interceptors, and instrumentation handlers.
// It supports dynamic registration, hot-reloading of configuration, and
// health-state awareness for each registered component.
package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ─── Component Kinds ─────────────────────────────────────────────────────────

// Kind classifies a registered component.
type Kind string

const (
	KindHTTPMiddleware  Kind = "http.middleware"
	KindGRPCInterceptor Kind = "grpc.interceptor"
	KindEventMiddleware Kind = "event.middleware"
	KindMetricCollector Kind = "metric.collector"
	KindTracer          Kind = "tracer"
	KindPropagator      Kind = "propagator"
)

// ─── HealthState ─────────────────────────────────────────────────────────────

// HealthState is the operational state of a registered component.
type HealthState string

const (
	StateHealthy   HealthState = "healthy"
	StateDegraded  HealthState = "degraded"
	StateUnhealthy HealthState = "unhealthy"
	StateUnknown   HealthState = "unknown"
)

// ─── Component ───────────────────────────────────────────────────────────────

// Component describes a registered middleware or instrumentation component.
type Component struct {
	Name        string
	Kind        Kind
	Description string
	Version     string
	Tags        map[string]string

	// Value is the actual middleware/interceptor function or struct.
	Value interface{}

	// HealthFn is called periodically to assess component health.
	// If nil, the component is assumed always healthy.
	HealthFn func(ctx context.Context) error

	registeredAt time.Time
	health       HealthState
	lastChecked  time.Time
	lastErr      error
	mu           sync.RWMutex
}

// Health returns the cached health state of the component.
func (c *Component) Health() HealthState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.health
}

// LastError returns the last health check error, if any.
func (c *Component) LastError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastErr
}

// Snapshot returns an immutable view of the component's current state.
func (c *Component) Snapshot() ComponentSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ComponentSnapshot{
		Name:         c.Name,
		Kind:         c.Kind,
		Description:  c.Description,
		Version:      c.Version,
		Tags:         c.Tags,
		Health:       c.health,
		LastChecked:  c.lastChecked,
		LastError:    c.lastErr,
		RegisteredAt: c.registeredAt,
	}
}

// ComponentSnapshot is an immutable view of a component's state.
type ComponentSnapshot struct {
	Name         string
	Kind         Kind
	Description  string
	Version      string
	Tags         map[string]string
	Health       HealthState
	LastChecked  time.Time
	LastError    error
	RegisteredAt time.Time
}

// ─── Registry ─────────────────────────────────────────────────────────────────

// Registry is a thread-safe, lifecycle-aware component registry.
type Registry struct {
	mu         sync.RWMutex
	components map[string]*Component
	log        *zap.Logger
	hooks      []LifecycleHook
	stopCh     chan struct{}
	healthInterval time.Duration
}

// LifecycleHook is called when components are registered or deregistered.
type LifecycleHook func(event string, snap ComponentSnapshot)

// Option configures the Registry.
type Option func(*Registry)

// WithLogger sets the registry logger.
func WithLogger(l *zap.Logger) Option {
	return func(r *Registry) { r.log = l }
}

// WithHealthInterval sets the health check polling interval. Default: 30s.
func WithHealthInterval(d time.Duration) Option {
	return func(r *Registry) { r.healthInterval = d }
}

// WithLifecycleHook registers a hook called on component registration events.
func WithLifecycleHook(h LifecycleHook) Option {
	return func(r *Registry) { r.hooks = append(r.hooks, h) }
}

// New creates a new Registry and starts the background health poller.
func New(opts ...Option) *Registry {
	r := &Registry{
		components:     make(map[string]*Component),
		log:            zap.NewNop(),
		stopCh:         make(chan struct{}),
		healthInterval: 30 * time.Second,
	}
	for _, o := range opts {
		o(r)
	}
	go r.pollHealth()
	return r
}

// Register adds a component to the registry. Returns an error if the name
// is already registered.
func (r *Registry) Register(c *Component) error {
	if c.Name == "" {
		return fmt.Errorf("registry: component name is required")
	}
	c.registeredAt = time.Now()
	c.health = StateUnknown

	r.mu.Lock()
	if _, exists := r.components[c.Name]; exists {
		r.mu.Unlock()
		return fmt.Errorf("registry: component %q is already registered", c.Name)
	}
	r.components[c.Name] = c
	r.mu.Unlock()

	r.log.Info("component registered",
		zap.String("name", c.Name),
		zap.String("kind", string(c.Kind)),
		zap.String("version", c.Version),
	)
	r.fireHooks("registered", c.Snapshot())

	// Run initial health check asynchronously.
	go r.checkHealth(c)
	return nil
}

// MustRegister panics if registration fails.
func (r *Registry) MustRegister(c *Component) {
	if err := r.Register(c); err != nil {
		panic(err)
	}
}

// Deregister removes a component by name.
func (r *Registry) Deregister(name string) error {
	r.mu.Lock()
	c, ok := r.components[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("registry: component %q not found", name)
	}
	delete(r.components, name)
	r.mu.Unlock()

	r.log.Info("component deregistered", zap.String("name", name))
	r.fireHooks("deregistered", c.Snapshot())
	return nil
}

// Get retrieves a component by name.
func (r *Registry) Get(name string) (*Component, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.components[name]
	return c, ok
}

// GetByKind returns all components of the given kind.
func (r *Registry) GetByKind(kind Kind) []*Component {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Component
	for _, c := range r.components {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// GetByTag returns all components that have the given tag key-value pair.
func (r *Registry) GetByTag(key, value string) []*Component {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Component
	for _, c := range r.components {
		if c.Tags[key] == value {
			out = append(out, c)
		}
	}
	return out
}

// Snapshots returns immutable snapshots of all registered components.
func (r *Registry) Snapshots() []ComponentSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ComponentSnapshot, 0, len(r.components))
	for _, c := range r.components {
		out = append(out, c.Snapshot())
	}
	return out
}

// IsHealthy returns true if all components with a HealthFn are healthy.
func (r *Registry) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.components {
		if c.HealthFn == nil {
			continue
		}
		if h := c.Health(); h == StateUnhealthy {
			return false
		}
	}
	return true
}

// Shutdown stops the background health poller.
func (r *Registry) Shutdown() {
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
}

func (r *Registry) pollHealth() {
	ticker := time.NewTicker(r.healthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.mu.RLock()
			components := make([]*Component, 0, len(r.components))
			for _, c := range r.components {
				components = append(components, c)
			}
			r.mu.RUnlock()
			for _, c := range components {
				go r.checkHealth(c)
			}
		}
	}
}

func (r *Registry) checkHealth(c *Component) {
	if c.HealthFn == nil {
		c.mu.Lock()
		c.health = StateHealthy
		c.lastChecked = time.Now()
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.HealthFn(ctx)
	c.mu.Lock()
	c.lastChecked = time.Now()
	c.lastErr = err
	if err != nil {
		c.health = StateUnhealthy
		r.log.Warn("component health check failed",
			zap.String("name", c.Name),
			zap.Error(err),
		)
	} else {
		c.health = StateHealthy
	}
	c.mu.Unlock()
}

func (r *Registry) fireHooks(event string, snap ComponentSnapshot) {
	for _, h := range r.hooks {
		go h(event, snap)
	}
}
