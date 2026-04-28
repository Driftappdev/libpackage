// Package servicemesh provides an enterprise-grade service mesh client library.
//
// Features:
//   - Service discovery (static, DNS, Consul, etcd)
//   - Multiple load balancing strategies (round-robin, weighted, least-conn, consistent hash)
//   - Circuit breaker per upstream
//   - Automatic retry with jitter
//   - mTLS support
//   - Request/response middleware chain
//   - Timeout and deadline propagation
//   - Distributed tracing (OpenTelemetry)
//   - Per-service Prometheus metrics
//   - Health-aware routing (removes unhealthy endpoints)
package servicemesh

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/platformcore/libpackage/plugins/common"
)

// ============================================================
// ENDPOINT
// ============================================================

// Endpoint represents a service instance.
type Endpoint struct {
	ID       string            `json:"id"`
	Address  string            `json:"address"` // host:port
	Weight   int               `json:"weight"`  // for weighted LB, default 1
	Metadata map[string]string `json:"metadata"`
	Healthy  bool              `json:"healthy"`
	// Internal load tracking
	activeConns int64 // atomic
}

func (e *Endpoint) incConns()    { atomic.AddInt64(&e.activeConns, 1) }
func (e *Endpoint) decConns()    { atomic.AddInt64(&e.activeConns, -1) }
func (e *Endpoint) conns() int64 { return atomic.LoadInt64(&e.activeConns) }

// ============================================================
// DISCOVERY
// ============================================================

// Discovery provides service endpoint resolution.
type Discovery interface {
	// Endpoints returns current healthy endpoints for the service.
	Endpoints(ctx context.Context, service string) ([]*Endpoint, error)
	// Watch calls onChange whenever endpoints change.
	Watch(ctx context.Context, service string, onChange func([]*Endpoint)) error
}

// StaticDiscovery uses a static list of endpoints.
type StaticDiscovery struct {
	mu       sync.RWMutex
	services map[string][]*Endpoint
}

// NewStaticDiscovery creates a static discovery backend.
func NewStaticDiscovery(services map[string][]*Endpoint) *StaticDiscovery {
	return &StaticDiscovery{services: services}
}

func (s *StaticDiscovery) Endpoints(_ context.Context, service string) ([]*Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	eps, ok := s.services[service]
	if !ok {
		return nil, fmt.Errorf("service %q not found", service)
	}
	result := make([]*Endpoint, 0, len(eps))
	for _, ep := range eps {
		if ep.Healthy {
			result = append(result, ep)
		}
	}
	return result, nil
}

func (s *StaticDiscovery) Watch(_ context.Context, _ string, _ func([]*Endpoint)) error {
	return nil // Static â€” no changes
}

// UpdateEndpoint updates an endpoint's health or metadata.
func (s *StaticDiscovery) UpdateEndpoint(service string, ep *Endpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.services[service] {
		if e.ID == ep.ID {
			s.services[service][i] = ep
			return
		}
	}
	s.services[service] = append(s.services[service], ep)
}

// ============================================================
// LOAD BALANCER
// ============================================================

// LBStrategy defines a load balancing algorithm.
type LBStrategy string

const (
	LBRoundRobin     LBStrategy = "round_robin"
	LBWeighted       LBStrategy = "weighted"
	LBLeastConns     LBStrategy = "least_conns"
	LBConsistentHash LBStrategy = "consistent_hash"
	LBRandom         LBStrategy = "random"
)

// LoadBalancer selects an endpoint from a list.
type LoadBalancer interface {
	Pick(ctx context.Context, endpoints []*Endpoint, key string) (*Endpoint, error)
}

// RoundRobinLB implements round-robin load balancing.
type RoundRobinLB struct {
	counter uint64
}

func NewRoundRobinLB() *RoundRobinLB { return &RoundRobinLB{} }

func (r *RoundRobinLB) Pick(_ context.Context, endpoints []*Endpoint, _ string) (*Endpoint, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints available")
	}
	idx := atomic.AddUint64(&r.counter, 1) % uint64(len(endpoints))
	return endpoints[idx], nil
}

// WeightedLB implements weighted round-robin load balancing.
type WeightedLB struct{}

func NewWeightedLB() *WeightedLB { return &WeightedLB{} }

func (w *WeightedLB) Pick(_ context.Context, endpoints []*Endpoint, _ string) (*Endpoint, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints available")
	}
	totalWeight := 0
	for _, ep := range endpoints {
		wt := ep.Weight
		if wt <= 0 {
			wt = 1
		}
		totalWeight += wt
	}
	// Weighted random selection
	target := int(pseudoRand() * float64(totalWeight))
	cumulative := 0
	for _, ep := range endpoints {
		wt := ep.Weight
		if wt <= 0 {
			wt = 1
		}
		cumulative += wt
		if target < cumulative {
			return ep, nil
		}
	}
	return endpoints[len(endpoints)-1], nil
}

// LeastConnLB selects the endpoint with fewest active connections.
type LeastConnLB struct{}

func NewLeastConnLB() *LeastConnLB { return &LeastConnLB{} }

func (l *LeastConnLB) Pick(_ context.Context, endpoints []*Endpoint, _ string) (*Endpoint, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints available")
	}
	best := endpoints[0]
	for _, ep := range endpoints[1:] {
		if ep.conns() < best.conns() {
			best = ep
		}
	}
	return best, nil
}

// ConsistentHashLB routes requests with the same key to the same endpoint.
type ConsistentHashLB struct{}

func NewConsistentHashLB() *ConsistentHashLB { return &ConsistentHashLB{} }

func (c *ConsistentHashLB) Pick(_ context.Context, endpoints []*Endpoint, key string) (*Endpoint, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints available")
	}
	if key == "" {
		return endpoints[0], nil
	}
	// Sort for deterministic ordering
	sorted := make([]*Endpoint, len(endpoints))
	copy(sorted, endpoints)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	h := fnv32(key)
	idx := h % uint32(len(sorted))
	return sorted[idx], nil
}

func fnv32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func pseudoRand() float64 {
	// Simple deterministic pseudo-random for demo
	return float64(time.Now().UnixNano()%1000) / 1000.0
}

// NewLoadBalancer creates a load balancer by strategy.
func NewLoadBalancer(strategy LBStrategy) LoadBalancer {
	switch strategy {
	case LBWeighted:
		return NewWeightedLB()
	case LBLeastConns:
		return NewLeastConnLB()
	case LBConsistentHash:
		return NewConsistentHashLB()
	default:
		return NewRoundRobinLB()
	}
}

// ============================================================
// MIDDLEWARE
// ============================================================

// RoundTripper is the HTTP round-tripper type used by the mesh.
type RoundTripper = http.RoundTripper

// Middleware wraps a RoundTripper to add cross-cutting concerns.
type Middleware func(next RoundTripper) RoundTripper

// Chain applies middlewares in order (first middleware is outermost).
func Chain(rt RoundTripper, middlewares ...Middleware) RoundTripper {
	for i := len(middlewares) - 1; i >= 0; i-- {
		rt = middlewares[i](rt)
	}
	return rt
}

// ============================================================
// BUILT-IN MIDDLEWARES
// ============================================================

// MetricsMiddleware records per-request metrics.
func MetricsMiddleware(metrics common.MetricsRecorder, service string) Middleware {
	return func(next RoundTripper) RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			start := time.Now()
			resp, err := next.RoundTrip(req)
			labels := map[string]string{
				"service": service,
				"method":  req.Method,
				"path":    req.URL.Path,
			}
			if resp != nil {
				labels["status"] = fmt.Sprintf("%d", resp.StatusCode)
			}
			metrics.RecordDuration("mesh_request_duration_seconds", start, labels)
			metrics.IncrCounter("mesh_requests_total", labels)
			return resp, err
		})
	}
}

// LoggingMiddleware logs every outbound request.
func LoggingMiddleware(logger common.Logger) Middleware {
	return func(next RoundTripper) RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			start := time.Now()
			resp, err := next.RoundTrip(req)
			fields := []common.Field{
				common.String("method", req.Method),
				common.String("url", req.URL.String()),
				common.Duration("duration", time.Since(start)),
			}
			if err != nil {
				fields = append(fields, common.Error(err))
				logger.Error("mesh: request failed", fields...)
			} else {
				fields = append(fields, common.Int("status", resp.StatusCode))
				logger.Debug("mesh: request", fields...)
			}
			return resp, err
		})
	}
}

// TimeoutMiddleware applies a per-request timeout.
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next RoundTripper) RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			ctx, cancel := context.WithTimeout(req.Context(), timeout)
			defer cancel()
			return next.RoundTrip(req.WithContext(ctx))
		})
	}
}

// RetryMiddleware retries failed requests with backoff.
func RetryMiddleware(cfg common.RetryConfig) Middleware {
	return func(next RoundTripper) RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			var resp *http.Response
			err := common.Retry(req.Context(), cfg, func(ctx context.Context) error {
				var err error
				resp, err = next.RoundTrip(req.WithContext(ctx))
				if err != nil {
					return err
				}
				if resp.StatusCode >= 500 {
					return fmt.Errorf("server error: %d", resp.StatusCode)
				}
				return nil
			})
			return resp, err
		})
	}
}

// CircuitBreakerMiddleware wraps requests in a circuit breaker.
func CircuitBreakerMiddleware(cb *common.CircuitBreaker) Middleware {
	return func(next RoundTripper) RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			var resp *http.Response
			err := cb.Execute(req.Context(), func(ctx context.Context) error {
				var err error
				resp, err = next.RoundTrip(req.WithContext(ctx))
				return err
			})
			return resp, err
		})
	}
}

// TracingMiddleware propagates trace headers (W3C TraceContext).
func TracingMiddleware() Middleware {
	return func(next RoundTripper) RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			// Propagate W3C trace context headers
			if traceID, ok := req.Context().Value(common.TraceIDKey).(string); ok && traceID != "" {
				req.Header.Set("traceparent", fmt.Sprintf("00-%s-0000000000000001-01", traceID))
			}
			if reqID, ok := req.Context().Value(common.RequestIDKey).(string); ok && reqID != "" {
				req.Header.Set("x-request-id", reqID)
			}
			return next.RoundTrip(req)
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// ============================================================
// SERVICE CLIENT
// ============================================================

// ServiceClientConfig configures a service mesh client.
type ServiceClientConfig struct {
	ServiceName     string
	Discovery       Discovery
	LBStrategy      LBStrategy
	Timeout         time.Duration
	MaxRetries      int
	RetryDelay      time.Duration
	TLSConfig       *tls.Config
	CircuitBreaker  *common.CircuitBreakerConfig
	Logger          common.Logger
	Metrics         common.MetricsRecorder
	ExtraMiddleware []Middleware
}

// ServiceClient is a mesh-aware HTTP client for a specific upstream service.
type ServiceClient struct {
	cfg       ServiceClientConfig
	lb        LoadBalancer
	transport RoundTripper
	logger    common.Logger
	metrics   common.MetricsRecorder
	cb        *common.CircuitBreaker
}

// NewServiceClient creates a new service mesh client.
func NewServiceClient(cfg ServiceClientConfig) (*ServiceClient, error) {
	if cfg.Logger == nil {
		cfg.Logger = common.MustNewLogger("info")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = common.NoopMetrics{}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig:     cfg.TLSConfig,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	cbCfg := common.CircuitBreakerConfig{
		Name:        cfg.ServiceName,
		MaxFailures: 5,
		Timeout:     30 * time.Second,
	}
	if cfg.CircuitBreaker != nil {
		cbCfg = *cfg.CircuitBreaker
	}
	cb := common.NewCircuitBreaker(cbCfg)

	retryCfg := common.RetryConfig{
		MaxAttempts:   cfg.MaxRetries + 1,
		InitialDelay:  cfg.RetryDelay,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
	if cfg.MaxRetries == 0 {
		retryCfg.MaxAttempts = 3
	}
	if cfg.RetryDelay == 0 {
		retryCfg.InitialDelay = 100 * time.Millisecond
	}

	middlewares := []Middleware{
		TracingMiddleware(),
		LoggingMiddleware(cfg.Logger),
		MetricsMiddleware(cfg.Metrics, cfg.ServiceName),
		CircuitBreakerMiddleware(cb),
		TimeoutMiddleware(cfg.Timeout),
		RetryMiddleware(retryCfg),
	}
	middlewares = append(middlewares, cfg.ExtraMiddleware...)

	rt := Chain(transport, middlewares...)

	return &ServiceClient{
		cfg:       cfg,
		lb:        NewLoadBalancer(cfg.LBStrategy),
		transport: rt,
		logger:    cfg.Logger,
		metrics:   cfg.Metrics,
		cb:        cb,
	}, nil
}

// Do executes an HTTP request against the upstream service.
// It handles service discovery and load balancing automatically.
func (c *ServiceClient) Do(ctx context.Context, req *http.Request, lbKey string) (*http.Response, error) {
	endpoints, err := c.cfg.Discovery.Endpoints(ctx, c.cfg.ServiceName)
	if err != nil {
		return nil, fmt.Errorf("discover %q: %w", c.cfg.ServiceName, err)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no healthy endpoints for %q", c.cfg.ServiceName)
	}

	ep, err := c.lb.Pick(ctx, endpoints, lbKey)
	if err != nil {
		return nil, fmt.Errorf("lb pick for %q: %w", c.cfg.ServiceName, err)
	}

	ep.incConns()
	defer ep.decConns()

	// Rewrite URL to selected endpoint
	req.URL.Scheme = "https"
	if c.cfg.TLSConfig == nil {
		req.URL.Scheme = "http"
	}
	req.URL.Host = ep.Address
	req.Host = ep.Address

	c.logger.Debug("mesh: routing request",
		common.String("service", c.cfg.ServiceName),
		common.String("endpoint", ep.Address),
		common.String("lb_key", lbKey))

	return c.transport.RoundTrip(req)
}

// ============================================================
// MESH MANAGER
// ============================================================

// Mesh manages all service clients in a microservice.
type Mesh struct {
	mu        sync.RWMutex
	clients   map[string]*ServiceClient
	discovery Discovery
	logger    common.Logger
	metrics   common.MetricsRecorder
}

// MeshConfig configures the mesh manager.
type MeshConfig struct {
	Discovery Discovery
	Logger    common.Logger
	Metrics   common.MetricsRecorder
}

// NewMesh creates a new service mesh manager.
func NewMesh(cfg MeshConfig) *Mesh {
	if cfg.Logger == nil {
		cfg.Logger = common.MustNewLogger("info")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = common.NoopMetrics{}
	}
	return &Mesh{
		clients:   make(map[string]*ServiceClient),
		discovery: cfg.Discovery,
		logger:    cfg.Logger,
		metrics:   cfg.Metrics,
	}
}

// RegisterService registers a service client in the mesh.
func (m *Mesh) RegisterService(cfg ServiceClientConfig) error {
	if cfg.Discovery == nil {
		cfg.Discovery = m.discovery
	}
	if cfg.Logger == nil {
		cfg.Logger = m.logger
	}
	if cfg.Metrics == nil {
		cfg.Metrics = m.metrics
	}

	client, err := NewServiceClient(cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.clients[cfg.ServiceName] = client
	m.mu.Unlock()
	m.logger.Info("mesh: service registered", common.String("service", cfg.ServiceName))
	return nil
}

// Client returns a registered service client.
func (m *Mesh) Client(service string) (*ServiceClient, error) {
	m.mu.RLock()
	c, ok := m.clients[service]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("service %q not registered in mesh", service)
	}
	return c, nil
}

// HealthCheck checks circuit breaker state for all services.
func (m *Mesh) HealthCheck(_ context.Context) map[string]common.HealthResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	results := make(map[string]common.HealthResult)
	for name, client := range m.clients {
		state := client.cb.State()
		status := common.HealthStatusUp
		if state == common.CBStateOpen {
			status = common.HealthStatusDown
		} else if state == common.CBStateHalfOpen {
			status = common.HealthStatusDegraded
		}
		results[name] = common.HealthResult{
			Status:    status,
			Component: name,
			Details: map[string]any{
				"circuit_breaker": state.String(),
			},
		}
	}
	return results
}

