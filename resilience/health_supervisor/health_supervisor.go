package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// HealthStatus represents the health of a component.
type HealthStatus int32

const (
	StatusUnknown  HealthStatus = iota
	StatusHealthy               // all checks passing
	StatusDegraded              // some checks failing but service still operational
	StatusUnhealthy             // critical failure; service should not receive traffic
)

func (s HealthStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusDegraded:
		return "degraded"
	case StatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// HealthCheck is a function that checks a single health aspect.
type HealthCheck func(ctx context.Context) HealthCheckResult

// HealthCheckResult is the output of a health check.
type HealthCheckResult struct {
	Name    string
	Status  HealthStatus
	Message string
	Latency time.Duration
	Details map[string]any
}

// HealthSupervisorConfig configures the supervisor.
type HealthSupervisorConfig struct {
	// Name of this supervisor.
	Name string
	// CheckInterval is how often all checks are polled.
	CheckInterval time.Duration
	// CheckTimeout is the per-check execution timeout.
	CheckTimeout time.Duration
	// DegradedThreshold: >= this fraction of checks failing → Degraded.
	DegradedThreshold float64
	// UnhealthyThreshold: >= this fraction of checks failing → Unhealthy.
	UnhealthyThreshold float64
	// ConsecutiveFailures: how many consecutive failures before a check is
	// considered "failing" (hysteresis).
	ConsecutiveFailures int
	// ConsecutiveSuccesses: how many consecutive successes to recover.
	ConsecutiveSuccesses int
	// OnStatusChange is called when the overall status changes.
	OnStatusChange func(name string, old, new HealthStatus)
	// OnCheckResult is called after each individual check execution.
	OnCheckResult func(result HealthCheckResult)
}

// registeredCheck holds a check and its running state.
type registeredCheck struct {
	name      string
	fn        HealthCheck
	critical  bool // if true, failure → Unhealthy immediately

	mu             sync.Mutex
	lastResult     HealthCheckResult
	consecFails    int
	consecSucc     int
	failing        bool // hysteresis state
}

// HealthSupervisor manages a pool of health checks, aggregates status,
// and provides /healthz-style endpoint data. It uses hysteresis to avoid
// flapping between states.
type HealthSupervisor struct {
	cfg     HealthSupervisorConfig
	checks  []*registeredCheck
	mu      sync.RWMutex
	status  atomic.Int32
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// metrics
	totalChecks atomic.Int64
	failedChecks atomic.Int64
	stateChanges atomic.Int64
}

// NewHealthSupervisor creates and starts a HealthSupervisor.
func NewHealthSupervisor(cfg HealthSupervisorConfig) *HealthSupervisor {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 10 * time.Second
	}
	if cfg.CheckTimeout <= 0 {
		cfg.CheckTimeout = 5 * time.Second
	}
	if cfg.DegradedThreshold <= 0 {
		cfg.DegradedThreshold = 0.2
	}
	if cfg.UnhealthyThreshold <= 0 {
		cfg.UnhealthyThreshold = 0.5
	}
	if cfg.ConsecutiveFailures <= 0 {
		cfg.ConsecutiveFailures = 3
	}
	if cfg.ConsecutiveSuccesses <= 0 {
		cfg.ConsecutiveSuccesses = 2
	}
	if cfg.Name == "" {
		cfg.Name = "supervisor"
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &HealthSupervisor{cfg: cfg, ctx: ctx, cancel: cancel}
	s.status.Store(int32(StatusUnknown))
	s.wg.Add(1)
	go s.run()
	return s
}

// Register adds a health check. critical=true means a single failure
// forces the overall status to Unhealthy.
func (s *HealthSupervisor) Register(name string, fn HealthCheck, critical bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = append(s.checks, &registeredCheck{
		name:     name,
		fn:       fn,
		critical: critical,
	})
}

// RegisterFunc is a convenience wrapper using a simple bool-returning function.
func (s *HealthSupervisor) RegisterFunc(name string, fn func(ctx context.Context) error, critical bool) {
	s.Register(name, func(ctx context.Context) HealthCheckResult {
		err := fn(ctx)
		if err != nil {
			return HealthCheckResult{
				Name:    name,
				Status:  StatusUnhealthy,
				Message: err.Error(),
			}
		}
		return HealthCheckResult{Name: name, Status: StatusHealthy}
	}, critical)
}

func (s *HealthSupervisor) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.cfg.CheckInterval)
	defer ticker.Stop()

	// Run once immediately
	s.runAll()

	for {
		select {
		case <-ticker.C:
			s.runAll()
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *HealthSupervisor) runAll() {
	s.mu.RLock()
	checks := make([]*registeredCheck, len(s.checks))
	copy(checks, s.checks)
	s.mu.RUnlock()

	if len(checks) == 0 {
		s.setStatus(StatusHealthy)
		return
	}

	var wg sync.WaitGroup
	for _, c := range checks {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runOne(c)
		}()
	}
	wg.Wait()
	s.aggregate(checks)
}

func (s *HealthSupervisor) runOne(c *registeredCheck) {
	ctx, cancel := context.WithTimeout(s.ctx, s.cfg.CheckTimeout)
	defer cancel()

	start := time.Now()
	result := c.fn(ctx)
	result.Latency = time.Since(start)
	result.Name = c.name
	s.totalChecks.Add(1)

	c.mu.Lock()
	c.lastResult = result
	if result.Status != StatusHealthy {
		s.failedChecks.Add(1)
		c.consecSucc = 0
		c.consecFails++
		if c.consecFails >= s.cfg.ConsecutiveFailures {
			c.failing = true
		}
	} else {
		c.consecFails = 0
		c.consecSucc++
		if c.consecSucc >= s.cfg.ConsecutiveSuccesses {
			c.failing = false
		}
	}
	c.mu.Unlock()

	if s.cfg.OnCheckResult != nil {
		s.cfg.OnCheckResult(result)
	}
}

func (s *HealthSupervisor) aggregate(checks []*registeredCheck) {
	total := len(checks)
	failing := 0
	hasCriticalFailure := false

	for _, c := range checks {
		c.mu.Lock()
		f := c.failing
		crit := c.critical
		c.mu.Unlock()

		if f {
			failing++
			if crit {
				hasCriticalFailure = true
			}
		}
	}

	var newStatus HealthStatus
	fraction := float64(failing) / float64(total)

	switch {
	case hasCriticalFailure || fraction >= s.cfg.UnhealthyThreshold:
		newStatus = StatusUnhealthy
	case fraction >= s.cfg.DegradedThreshold:
		newStatus = StatusDegraded
	default:
		newStatus = StatusHealthy
	}

	s.setStatus(newStatus)
}

func (s *HealthSupervisor) setStatus(status HealthStatus) {
	old := HealthStatus(s.status.Swap(int32(status)))
	if old != status {
		s.stateChanges.Add(1)
		if s.cfg.OnStatusChange != nil {
			s.cfg.OnStatusChange(s.cfg.Name, old, status)
		}
	}
}

// Status returns the current aggregated health status.
func (s *HealthSupervisor) Status() HealthStatus {
	return HealthStatus(s.status.Load())
}

// IsHealthy returns true if status is Healthy or Degraded.
func (s *HealthSupervisor) IsHealthy() bool {
	return s.Status() != StatusUnhealthy
}

// Report returns the current status and per-check results for /healthz endpoints.
func (s *HealthSupervisor) Report() HealthReport {
	s.mu.RLock()
	checks := make([]*registeredCheck, len(s.checks))
	copy(checks, s.checks)
	s.mu.RUnlock()

	results := make([]HealthCheckResult, 0, len(checks))
	for _, c := range checks {
		c.mu.Lock()
		r := c.lastResult
		c.mu.Unlock()
		results = append(results, r)
	}
	return HealthReport{
		Name:      s.cfg.Name,
		Status:    s.Status(),
		Checks:    results,
		Timestamp: time.Now(),
		Stats: HealthSupervisorStats{
			TotalChecks:  s.totalChecks.Load(),
			FailedChecks: s.failedChecks.Load(),
			StateChanges: s.stateChanges.Load(),
		},
	}
}

// ForceCheck runs all checks immediately, synchronously.
func (s *HealthSupervisor) ForceCheck() HealthReport {
	s.runAll()
	return s.Report()
}

// Close stops the supervisor's polling loop.
func (s *HealthSupervisor) Close() {
	s.cancel()
	s.wg.Wait()
}

// HealthReport is the full health report returned by Report().
type HealthReport struct {
	Name      string
	Status    HealthStatus
	Checks    []HealthCheckResult
	Timestamp time.Time
	Stats     HealthSupervisorStats
}

// Summary returns a human-readable summary string.
func (r HealthReport) Summary() string {
	return fmt.Sprintf("[%s] %s at %s (%d checks, %d state changes)",
		r.Name, r.Status, r.Timestamp.Format(time.RFC3339),
		len(r.Checks), r.Stats.StateChanges)
}

// HealthSupervisorStats holds counters.
type HealthSupervisorStats struct {
	TotalChecks  int64
	FailedChecks int64
	StateChanges int64
}
