// Package common provides shared interfaces, utilities, and base types
// used across all enterprise toolkit modules.
package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ============================================================
// LOGGER
// ============================================================

// Logger is the shared structured logger interface.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
	WithContext(ctx context.Context) Logger
}

// Field represents a log field key-value pair.
type Field = zap.Field

// Common field constructors (re-export for convenience).
var (
	String   = zap.String
	Int      = zap.Int
	Int64    = zap.Int64
	Float64  = zap.Float64
	Bool     = zap.Bool
	Duration = zap.Duration
	Time     = zap.Time
	Error    = zap.Error
	Any      = zap.Any
)

type zapLogger struct {
	zl *zap.Logger
}

// NewLogger creates a production-ready structured logger.
func NewLogger(level string, opts ...LogOption) (Logger, error) {
	cfg := &logConfig{
		level:       level,
		development: false,
		encoding:    "json",
	}
	for _, o := range opts {
		o(cfg)
	}

	zapLevel, err := zapcore.ParseLevel(cfg.level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.level, err)
	}

	var zapCfg zap.Config
	if cfg.development {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	zapCfg.Level = zap.NewAtomicLevelAt(zapLevel)
	zapCfg.Encoding = cfg.encoding

	zl, err := zapCfg.Build(zap.AddCallerSkip(1))
	if err != nil {
		return nil, err
	}
	return &zapLogger{zl: zl}, nil
}

// MustNewLogger panics on error — use in main/init.
func MustNewLogger(level string, opts ...LogOption) Logger {
	l, err := NewLogger(level, opts...)
	if err != nil {
		panic(err)
	}
	return l
}

type logConfig struct {
	level       string
	development bool
	encoding    string
}

// LogOption configures the logger.
type LogOption func(*logConfig)

func WithDevelopment() LogOption     { return func(c *logConfig) { c.development = true } }
func WithConsoleEncoding() LogOption { return func(c *logConfig) { c.encoding = "console" } }

func (l *zapLogger) Debug(msg string, fields ...Field) { l.zl.Debug(msg, fields...) }
func (l *zapLogger) Info(msg string, fields ...Field)  { l.zl.Info(msg, fields...) }
func (l *zapLogger) Warn(msg string, fields ...Field)  { l.zl.Warn(msg, fields...) }
func (l *zapLogger) Error(msg string, fields ...Field) { l.zl.Error(msg, fields...) }
func (l *zapLogger) Fatal(msg string, fields ...Field) { l.zl.Fatal(msg, fields...) }
func (l *zapLogger) With(fields ...Field) Logger       { return &zapLogger{zl: l.zl.With(fields...)} }
func (l *zapLogger) WithContext(ctx context.Context) Logger {
	fields := extractContextFields(ctx)
	return &zapLogger{zl: l.zl.With(fields...)}
}

// Context key types.
type contextKey string

const (
	TraceIDKey   contextKey = "trace_id"
	SpanIDKey    contextKey = "span_id"
	RequestIDKey contextKey = "request_id"
	ServiceKey   contextKey = "service"
	TenantIDKey  contextKey = "tenant_id"
)

func extractContextFields(ctx context.Context) []Field {
	var fields []Field
	if v, ok := ctx.Value(TraceIDKey).(string); ok && v != "" {
		fields = append(fields, zap.String("trace_id", v))
	}
	if v, ok := ctx.Value(RequestIDKey).(string); ok && v != "" {
		fields = append(fields, zap.String("request_id", v))
	}
	if v, ok := ctx.Value(TenantIDKey).(string); ok && v != "" {
		fields = append(fields, zap.String("tenant_id", v))
	}
	return fields
}

// WithTraceID injects a trace ID into the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

// WithRequestID injects a request ID into the context.
func WithRequestID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, reqID)
}

// WithTenantID injects a tenant ID into the context.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, TenantIDKey, tenantID)
}

// ============================================================
// METRICS
// ============================================================

// MetricsRecorder abstracts Prometheus (or any metrics backend).
type MetricsRecorder interface {
	IncrCounter(name string, labels map[string]string)
	RecordGauge(name string, value float64, labels map[string]string)
	RecordHistogram(name string, value float64, labels map[string]string)
	RecordDuration(name string, start time.Time, labels map[string]string)
}

// NoopMetrics is a no-operation metrics recorder.
type NoopMetrics struct{}

func (NoopMetrics) IncrCounter(_ string, _ map[string]string)                 {}
func (NoopMetrics) RecordGauge(_ string, _ float64, _ map[string]string)      {}
func (NoopMetrics) RecordHistogram(_ string, _ float64, _ map[string]string)  {}
func (NoopMetrics) RecordDuration(_ string, _ time.Time, _ map[string]string) {}

// ============================================================
// HEALTH CHECK
// ============================================================

// HealthStatus represents a component's health state.
type HealthStatus string

const (
	HealthStatusUp       HealthStatus = "UP"
	HealthStatusDown     HealthStatus = "DOWN"
	HealthStatusDegraded HealthStatus = "DEGRADED"
)

// HealthChecker is implemented by any component that can report health.
type HealthChecker interface {
	HealthCheck(ctx context.Context) HealthResult
}

// HealthResult holds the result of a health check.
type HealthResult struct {
	Status    HealthStatus   `json:"status"`
	Component string         `json:"component"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	CheckedAt time.Time      `json:"checked_at"`
}

// HealthRegistry aggregates health checks for all components.
type HealthRegistry struct {
	mu       sync.RWMutex
	checkers map[string]HealthChecker
}

// NewHealthRegistry creates a new health registry.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{checkers: make(map[string]HealthChecker)}
}

// Register adds a health checker.
func (r *HealthRegistry) Register(name string, c HealthChecker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers[name] = c
}

// CheckAll runs all registered health checks concurrently.
func (r *HealthRegistry) CheckAll(ctx context.Context) map[string]HealthResult {
	r.mu.RLock()
	checkers := make(map[string]HealthChecker, len(r.checkers))
	for k, v := range r.checkers {
		checkers[k] = v
	}
	r.mu.RUnlock()

	results := make(map[string]HealthResult, len(checkers))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, checker := range checkers {
		wg.Add(1)
		go func(n string, c HealthChecker) {
			defer wg.Done()
			result := c.HealthCheck(ctx)
			result.Component = n
			result.CheckedAt = time.Now()
			mu.Lock()
			results[n] = result
			mu.Unlock()
		}(name, checker)
	}
	wg.Wait()
	return results
}

// OverallStatus computes the aggregate status.
func (r *HealthRegistry) OverallStatus(results map[string]HealthResult) HealthStatus {
	for _, res := range results {
		if res.Status == HealthStatusDown {
			return HealthStatusDown
		}
	}
	for _, res := range results {
		if res.Status == HealthStatusDegraded {
			return HealthStatusDegraded
		}
	}
	return HealthStatusUp
}

// HTTPHandler returns an HTTP handler for /health and /readiness endpoints.
func (r *HealthRegistry) HTTPHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		results := r.CheckAll(req.Context())
		overall := r.OverallStatus(results)
		resp := map[string]any{
			"status":     overall,
			"components": results,
			"timestamp":  time.Now(),
			"hostname":   hostname(),
		}
		w.Header().Set("Content-Type", "application/json")
		if overall == HealthStatusDown {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	mux.HandleFunc("/readiness", func(w http.ResponseWriter, req *http.Request) {
		results := r.CheckAll(req.Context())
		overall := r.OverallStatus(results)
		if overall == HealthStatusDown {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/liveness", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return mux
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// ============================================================
// CIRCUIT BREAKER
// ============================================================

// CBState represents a circuit breaker state.
type CBState int

const (
	CBStateClosed   CBState = iota // Normal operation
	CBStateOpen                    // Failing — reject calls
	CBStateHalfOpen                // Testing recovery
)

func (s CBState) String() string {
	switch s {
	case CBStateClosed:
		return "CLOSED"
	case CBStateOpen:
		return "OPEN"
	case CBStateHalfOpen:
		return "HALF_OPEN"
	}
	return "UNKNOWN"
}

// CircuitBreakerConfig configures a circuit breaker.
type CircuitBreakerConfig struct {
	Name            string
	MaxFailures     int
	Timeout         time.Duration // How long to stay OPEN
	HalfOpenMaxReqs int           // Max requests in HALF_OPEN state
	OnStateChange   func(name string, from, to CBState)
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	cfg          CircuitBreakerConfig
	mu           sync.Mutex
	state        CBState
	failures     int
	halfOpenReqs int
	openedAt     time.Time
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.MaxFailures == 0 {
		cfg.MaxFailures = 5
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.HalfOpenMaxReqs == 0 {
		cfg.HalfOpenMaxReqs = 3
	}
	return &CircuitBreaker{cfg: cfg}
}

// ErrCircuitOpen is returned when the circuit is open.
var ErrCircuitOpen = fmt.Errorf("circuit breaker is OPEN")

// Execute runs fn through the circuit breaker.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	if err := cb.allow(); err != nil {
		return err
	}
	err := fn(ctx)
	cb.record(err)
	return err
}

func (cb *CircuitBreaker) allow() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case CBStateOpen:
		if time.Since(cb.openedAt) > cb.cfg.Timeout {
			cb.transition(CBStateHalfOpen)
			return nil
		}
		return ErrCircuitOpen
	case CBStateHalfOpen:
		if cb.halfOpenReqs >= cb.cfg.HalfOpenMaxReqs {
			return ErrCircuitOpen
		}
		cb.halfOpenReqs++
	}
	return nil
}

func (cb *CircuitBreaker) record(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if err != nil {
		cb.failures++
		if cb.state == CBStateHalfOpen || cb.failures >= cb.cfg.MaxFailures {
			cb.transition(CBStateOpen)
		}
	} else {
		if cb.state == CBStateHalfOpen {
			cb.transition(CBStateClosed)
		}
		cb.failures = 0
	}
}

func (cb *CircuitBreaker) transition(to CBState) {
	from := cb.state
	cb.state = to
	cb.halfOpenReqs = 0
	if to == CBStateOpen {
		cb.openedAt = time.Now()
	}
	if cb.cfg.OnStateChange != nil && from != to {
		go cb.cfg.OnStateChange(cb.cfg.Name, from, to)
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() CBState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// ============================================================
// RETRY
// ============================================================

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	Jitter          bool
	RetryableErrors func(error) bool
}

// DefaultRetryConfig returns sensible retry defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
}

// Retry executes fn with exponential backoff retry.
func Retry(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	delay := cfg.InitialDelay
	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled after %d attempts: %w", attempt, err)
		}
		if err := fn(ctx); err != nil {
			lastErr = err
			if cfg.RetryableErrors != nil && !cfg.RetryableErrors(err) {
				return err
			}
			if attempt < cfg.MaxAttempts-1 {
				sleep := delay
				if cfg.Jitter {
					sleep = addJitter(sleep)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(sleep):
				}
				delay = time.Duration(float64(delay) * cfg.BackoffFactor)
				if delay > cfg.MaxDelay {
					delay = cfg.MaxDelay
				}
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("all %d attempts failed, last error: %w", cfg.MaxAttempts, lastErr)
}

func addJitter(d time.Duration) time.Duration {
	// Add ±20% jitter
	jitter := time.Duration(float64(d) * 0.2)
	return d - jitter + time.Duration(float64(jitter*2)*pseudoRand())
}

var randMu sync.Mutex
var randVal uint64 = 12345

func pseudoRand() float64 {
	randMu.Lock()
	randVal ^= randVal << 13
	randVal ^= randVal >> 7
	randVal ^= randVal << 17
	v := randVal
	randMu.Unlock()
	return float64(v%1000) / 1000.0
}
