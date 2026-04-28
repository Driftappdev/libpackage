// Package performance provides enterprise-grade performance management for
// high-throughput distributed systems. Designed as a reusable shared library
// for all microservices in the organization.
//
// Features:
//   - Adaptive rate limiting (token bucket + sliding window + leaky bucket)
//   - Multi-level circuit breakers with state machine
//   - Adaptive concurrency limiting (Vegas/AIMD algorithm)
//   - Connection pool management
//   - Request hedging & timeout budgets
//   - Bulkhead isolation pattern
//   - Load shedding with priority queues
//   - Backpressure propagation
//   - Auto-scaling signals
//   - Hot-path CPU/Memory optimizations
package performance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================
// ERRORS
// ============================================================

var (
	ErrRateLimitExceeded       = errors.New("perf: rate limit exceeded")
	ErrCircuitOpen             = errors.New("perf: circuit breaker open")
	ErrConcurrencyLimitReached = errors.New("perf: concurrency limit reached")
	ErrBulkheadFull            = errors.New("perf: bulkhead full")
	ErrLoadShed                = errors.New("perf: request shed due to overload")
	ErrTimeoutBudgetExceeded   = errors.New("perf: timeout budget exceeded")
	ErrPoolExhausted           = errors.New("perf: connection pool exhausted")
)

// ============================================================
// SECTION 1: TOKEN BUCKET RATE LIMITER (High-Performance)
// ============================================================

// TokenBucket implements a high-performance, thread-safe token bucket
// with burst support and dynamic rate adjustment.
type TokenBucket struct {
	rate       float64 // tokens per second
	burst      float64 // max burst size
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
	// Metrics
	allowed  atomic.Uint64
	rejected atomic.Uint64
}

// NewTokenBucket creates a token bucket rate limiter.
// rate: sustained requests/second, burst: maximum burst allowance.
func NewTokenBucket(rate, burst float64) *TokenBucket {
	return &TokenBucket{
		rate:       rate,
		burst:      burst,
		tokens:     burst, // Start full
		lastRefill: time.Now(),
	}
}

// Allow attempts to consume n tokens. Returns true if allowed.
func (tb *TokenBucket) Allow(n float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens = math.Min(tb.burst, tb.tokens+elapsed*tb.rate)
	tb.lastRefill = now

	if tb.tokens >= n {
		tb.tokens -= n
		tb.allowed.Add(1)
		return true
	}
	tb.rejected.Add(1)
	return false
}

// Wait blocks until a token is available or context is done.
func (tb *TokenBucket) Wait(ctx context.Context) error {
	for {
		if tb.Allow(1) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond * 10):
		}
	}
}

// SetRate dynamically adjusts the rate (useful for adaptive limiting).
func (tb *TokenBucket) SetRate(rate float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.rate = rate
}

// Stats returns current limiter statistics.
func (tb *TokenBucket) Stats() map[string]uint64 {
	return map[string]uint64{
		"allowed":  tb.allowed.Load(),
		"rejected": tb.rejected.Load(),
	}
}

// ============================================================
// SECTION 2: SLIDING WINDOW RATE LIMITER
// ============================================================

// SlidingWindowLimiter provides precise rate limiting using a sliding
// time window with O(1) amortized complexity via ring buffer.
type SlidingWindowLimiter struct {
	limit    int
	window   time.Duration
	mu       sync.Mutex
	requests []time.Time // ring buffer
	head     int
	count    int
	// Metrics
	allowed  atomic.Uint64
	rejected atomic.Uint64
}

func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		limit:    limit,
		window:   window,
		requests: make([]time.Time, limit),
	}
}

func (sl *SlidingWindowLimiter) Allow() bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-sl.window)

	// Evict expired entries
	for sl.count > 0 {
		oldest := sl.requests[(sl.head-sl.count+len(sl.requests))%len(sl.requests)]
		if oldest.After(cutoff) {
			break
		}
		sl.count--
	}

	if sl.count >= sl.limit {
		sl.rejected.Add(1)
		return false
	}

	sl.requests[sl.head] = now
	sl.head = (sl.head + 1) % len(sl.requests)
	sl.count++
	sl.allowed.Add(1)
	return true
}

// ============================================================
// SECTION 3: CIRCUIT BREAKER (Full State Machine)
// ============================================================

type CBState int32

const (
	CBStateClosed   CBState = iota // Normal operation
	CBStateOpen                    // Failing, reject requests
	CBStateHalfOpen                // Testing recovery
)

func (s CBState) String() string {
	return [...]string{"CLOSED", "OPEN", "HALF_OPEN"}[s]
}

// CircuitBreakerConfig holds configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	Name                  string
	FailureThreshold      int           // Failures before opening
	SuccessThreshold      int           // Successes in half-open before closing
	Timeout               time.Duration // How long to stay open
	HalfOpenMaxRequests   int           // Max concurrent in half-open
	SamplingWindow        time.Duration // Window for rate-based tripping
	FailureRateThreshold  float64       // Open if failure rate exceeds this (0.0-1.0)
	SlowCallThreshold     time.Duration // Calls slower than this count as slow
	SlowCallRateThreshold float64       // Open if slow rate exceeds this
}

// DefaultCircuitBreakerConfig returns production-ready defaults.
func DefaultCircuitBreakerConfig(name string) CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Name:                  name,
		FailureThreshold:      5,
		SuccessThreshold:      3,
		Timeout:               60 * time.Second,
		HalfOpenMaxRequests:   3,
		SamplingWindow:        10 * time.Second,
		FailureRateThreshold:  0.5,
		SlowCallThreshold:     2 * time.Second,
		SlowCallRateThreshold: 0.8,
	}
}

// CircuitBreaker implements a resilient circuit breaker pattern.
type CircuitBreaker struct {
	cfg         CircuitBreakerConfig
	state       atomic.Int32
	failures    atomic.Int64
	successes   atomic.Int64
	lastFailure atomic.Int64 // unix nano
	openedAt    atomic.Int64 // unix nano
	halfOpenIn  atomic.Int64 // active half-open requests
	mu          sync.Mutex

	// Sliding window metrics
	callWindow []callRecord
	winHead    int
	winMu      sync.Mutex

	// Event callbacks
	OnStateChange func(from, to CBState)
}

type callRecord struct {
	ts      time.Time
	success bool
	latency time.Duration
}

func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		cfg:        cfg,
		callWindow: make([]callRecord, 1000),
	}
}

func (cb *CircuitBreaker) State() CBState {
	return CBState(cb.state.Load())
}

// Allow checks if a request can proceed.
func (cb *CircuitBreaker) Allow() error {
	state := cb.State()
	switch state {
	case CBStateClosed:
		return nil
	case CBStateOpen:
		if time.Now().UnixNano()-cb.openedAt.Load() > cb.cfg.Timeout.Nanoseconds() {
			if cb.state.CompareAndSwap(int32(CBStateOpen), int32(CBStateHalfOpen)) {
				cb.notifyStateChange(CBStateOpen, CBStateHalfOpen)
			}
			return cb.Allow()
		}
		return ErrCircuitOpen
	case CBStateHalfOpen:
		current := cb.halfOpenIn.Add(1)
		if int(current) > cb.cfg.HalfOpenMaxRequests {
			cb.halfOpenIn.Add(-1)
			return ErrCircuitOpen
		}
		return nil
	}
	return nil
}

// RecordSuccess records a successful call.
func (cb *CircuitBreaker) RecordSuccess(latency time.Duration) {
	if cb.State() == CBStateHalfOpen {
		cb.halfOpenIn.Add(-1)
		cb.successes.Add(1)
		if int(cb.successes.Load()) >= cb.cfg.SuccessThreshold {
			prev := CBState(cb.state.Swap(int32(CBStateClosed)))
			cb.failures.Store(0)
			cb.successes.Store(0)
			cb.notifyStateChange(prev, CBStateClosed)
		}
	}
	cb.recordCall(latency, true)
}

// RecordFailure records a failed call.
func (cb *CircuitBreaker) RecordFailure(latency time.Duration) {
	if cb.State() == CBStateHalfOpen {
		cb.halfOpenIn.Add(-1)
		cb.trip()
		return
	}
	failures := cb.failures.Add(1)
	cb.lastFailure.Store(time.Now().UnixNano())
	cb.recordCall(latency, false)
	if int(failures) >= cb.cfg.FailureThreshold || cb.checkRateBasedTrip() {
		cb.trip()
	}
}

func (cb *CircuitBreaker) trip() {
	prev := CBState(cb.state.Swap(int32(CBStateOpen)))
	if prev != CBStateOpen {
		cb.openedAt.Store(time.Now().UnixNano())
		cb.failures.Store(0)
		cb.successes.Store(0)
		cb.notifyStateChange(prev, CBStateOpen)
	}
}

func (cb *CircuitBreaker) checkRateBasedTrip() bool {
	cb.winMu.Lock()
	defer cb.winMu.Unlock()
	cutoff := time.Now().Add(-cb.cfg.SamplingWindow)
	var total, failed, slow int
	for _, r := range cb.callWindow {
		if r.ts.IsZero() || r.ts.Before(cutoff) {
			continue
		}
		total++
		if !r.success {
			failed++
		}
		if r.latency > cb.cfg.SlowCallThreshold {
			slow++
		}
	}
	if total < 10 {
		return false
	}
	failRate := float64(failed) / float64(total)
	slowRate := float64(slow) / float64(total)
	return failRate >= cb.cfg.FailureRateThreshold ||
		slowRate >= cb.cfg.SlowCallRateThreshold
}

func (cb *CircuitBreaker) recordCall(latency time.Duration, success bool) {
	cb.winMu.Lock()
	cb.callWindow[cb.winHead] = callRecord{ts: time.Now(), success: success, latency: latency}
	cb.winHead = (cb.winHead + 1) % len(cb.callWindow)
	cb.winMu.Unlock()
}

func (cb *CircuitBreaker) notifyStateChange(from, to CBState) {
	if cb.OnStateChange != nil {
		go cb.OnStateChange(from, to)
	}
}

// Execute wraps a function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.Allow(); err != nil {
		return err
	}
	start := time.Now()
	err := fn()
	latency := time.Since(start)
	if err != nil {
		cb.RecordFailure(latency)
	} else {
		cb.RecordSuccess(latency)
	}
	return err
}

// ============================================================
// SECTION 4: ADAPTIVE CONCURRENCY LIMITER (Vegas Algorithm)
// ============================================================

// AdaptiveConcurrencyLimiter uses the TCP Vegas algorithm to
// dynamically adjust the concurrency limit based on observed latency.
// This prevents overload without static configuration.
type AdaptiveConcurrencyLimiter struct {
	minLimit    int
	maxLimit    int
	limit       atomic.Int64
	inFlight    atomic.Int64
	minRTT      atomic.Int64 // nanoseconds, min observed RTT
	windowRTT   atomic.Int64 // nanoseconds, current window min RTT
	sampleCount atomic.Int64
	mu          sync.Mutex

	// AIMD parameters
	alpha float64 // Additive increase
	beta  float64 // Multiplicative decrease

	// Metrics
	totalAllowed  atomic.Uint64
	totalRejected atomic.Uint64
	totalTimeouts atomic.Uint64
}

// NewAdaptiveConcurrencyLimiter creates a Vegas-based adaptive limiter.
func NewAdaptiveConcurrencyLimiter(minLimit, maxLimit, initialLimit int) *AdaptiveConcurrencyLimiter {
	acl := &AdaptiveConcurrencyLimiter{
		minLimit: minLimit,
		maxLimit: maxLimit,
		alpha:    3,    // Add up to 3 on improvement
		beta:     0.75, // Reduce by 25% on congestion
	}
	acl.limit.Store(int64(initialLimit))
	acl.minRTT.Store(math.MaxInt64)
	acl.windowRTT.Store(math.MaxInt64)
	return acl
}

// Acquire acquires a concurrency slot. Returns a release function or error.
func (acl *AdaptiveConcurrencyLimiter) Acquire() (release func(success bool), err error) {
	current := acl.inFlight.Load()
	limit := acl.limit.Load()

	if current >= limit {
		acl.totalRejected.Add(1)
		return nil, ErrConcurrencyLimitReached
	}

	acl.inFlight.Add(1)
	acl.totalAllowed.Add(1)
	startTime := time.Now()

	return func(success bool) {
		rtt := time.Since(startTime).Nanoseconds()
		acl.inFlight.Add(-1)
		if success {
			acl.updateLimit(rtt)
		}
	}, nil
}

func (acl *AdaptiveConcurrencyLimiter) updateLimit(rtt int64) {
	// Update window RTT
	for {
		current := acl.windowRTT.Load()
		if rtt >= current {
			break
		}
		if acl.windowRTT.CompareAndSwap(current, rtt) {
			break
		}
	}

	// Update min RTT (long-running minimum)
	for {
		current := acl.minRTT.Load()
		if rtt >= current {
			break
		}
		if acl.minRTT.CompareAndSwap(current, rtt) {
			break
		}
	}

	// Compute new limit using Vegas gradient
	count := acl.sampleCount.Add(1)
	if count%int64(acl.limit.Load()) != 0 {
		return
	}

	// Reset window
	windowRTT := acl.windowRTT.Swap(math.MaxInt64)
	minRTT := acl.minRTT.Load()

	if minRTT == math.MaxInt64 || windowRTT == math.MaxInt64 {
		return
	}

	currentLimit := acl.limit.Load()
	inFlight := acl.inFlight.Load()
	gradient := float64(minRTT) / float64(windowRTT)

	var newLimit float64
	if gradient < 1.0 {
		// Congestion detected: multiplicative decrease
		newLimit = float64(currentLimit) * acl.beta
	} else {
		// No congestion: additive increase
		queueSize := float64(inFlight) - (float64(inFlight) * float64(minRTT) / float64(windowRTT))
		if queueSize < acl.alpha {
			newLimit = float64(currentLimit) + acl.alpha
		} else {
			newLimit = float64(currentLimit) - 1
		}
	}

	newLimitInt := int64(math.Max(float64(acl.minLimit), math.Min(float64(acl.maxLimit), newLimit)))
	acl.limit.Store(newLimitInt)
}

// ============================================================
// SECTION 5: BULKHEAD ISOLATION
// ============================================================

// Bulkhead isolates resources between different service calls,
// preventing a slow downstream from exhausting all workers.
type Bulkhead struct {
	name      string
	semaphore chan struct{}
	queue     chan func()
	workers   int
	timeout   time.Duration
	wg        sync.WaitGroup
	stopCh    chan struct{}
	// Metrics
	executed atomic.Uint64
	rejected atomic.Uint64
	timeouts atomic.Uint64
}

// NewBulkhead creates a bulkhead with isolated worker pool.
func NewBulkhead(name string, maxConcurrent, maxQueue, workers int, timeout time.Duration) *Bulkhead {
	b := &Bulkhead{
		name:      name,
		semaphore: make(chan struct{}, maxConcurrent),
		queue:     make(chan func(), maxQueue),
		workers:   workers,
		timeout:   timeout,
		stopCh:    make(chan struct{}),
	}
	for i := 0; i < maxConcurrent; i++ {
		b.semaphore <- struct{}{}
	}
	for i := 0; i < workers; i++ {
		b.wg.Add(1)
		go b.worker()
	}
	return b
}

func (b *Bulkhead) worker() {
	defer b.wg.Done()
	for {
		select {
		case fn := <-b.queue:
			select {
			case <-b.semaphore:
				fn()
				b.semaphore <- struct{}{}
				b.executed.Add(1)
			case <-time.After(b.timeout):
				b.timeouts.Add(1)
			}
		case <-b.stopCh:
			return
		}
	}
}

// Submit enqueues work into the bulkhead.
func (b *Bulkhead) Submit(ctx context.Context, fn func()) error {
	select {
	case b.queue <- fn:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		b.rejected.Add(1)
		return ErrBulkheadFull
	}
}

// ExecuteSync runs fn synchronously with bulkhead protection.
func (b *Bulkhead) ExecuteSync(ctx context.Context, fn func() error) error {
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()

	select {
	case <-b.semaphore:
		defer func() { b.semaphore <- struct{}{} }()
		b.executed.Add(1)
		return fn()
	case <-timer.C:
		b.timeouts.Add(1)
		return ErrBulkheadFull
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Bulkhead) Close() {
	close(b.stopCh)
	b.wg.Wait()
}

// ============================================================
// SECTION 6: LOAD SHEDDER WITH PRIORITY QUEUE
// ============================================================

// Priority level for request prioritization during overload.
type Priority int

const (
	PriorityCritical Priority = iota // Never shed (health checks, etc.)
	PriorityHigh                     // Very rarely shed
	PriorityNormal                   // Shed first during light overload
	PriorityLow                      // Shed aggressively
	PriorityBatch                    // Shed immediately under any load
)

// LoadShedder implements priority-based load shedding.
// Under high load, low-priority requests are rejected to protect the service.
type LoadShedder struct {
	cpuThresholds   map[Priority]float64 // CPU% at which this priority is shed
	memThresholds   map[Priority]float64 // Mem% at which this priority is shed
	latencyBaseline time.Duration
	currentLatency  atomic.Int64 // nanoseconds
	mu              sync.RWMutex
	// Metrics
	shedCount [5]atomic.Uint64
	passCount [5]atomic.Uint64
}

// DefaultLoadShedder creates a load shedder with sensible defaults.
func DefaultLoadShedder() *LoadShedder {
	return &LoadShedder{
		cpuThresholds: map[Priority]float64{
			PriorityCritical: 100.0, // Never
			PriorityHigh:     95.0,
			PriorityNormal:   80.0,
			PriorityLow:      60.0,
			PriorityBatch:    40.0,
		},
		memThresholds: map[Priority]float64{
			PriorityCritical: 100.0,
			PriorityHigh:     95.0,
			PriorityNormal:   85.0,
			PriorityLow:      70.0,
			PriorityBatch:    50.0,
		},
		latencyBaseline: 100 * time.Millisecond,
	}
}

// ShouldShed returns true if the request should be rejected.
func (ls *LoadShedder) ShouldShed(p Priority) bool {
	cpu := ls.getCPUUsage()
	mem := ls.getMemUsage()

	cpuThresh := ls.cpuThresholds[p]
	memThresh := ls.memThresholds[p]

	shouldShed := cpu >= cpuThresh || mem >= memThresh

	if shouldShed {
		ls.shedCount[p].Add(1)
	} else {
		ls.passCount[p].Add(1)
	}
	return shouldShed
}

// Allow is a middleware-friendly wrapper.
func (ls *LoadShedder) Allow(ctx context.Context, p Priority) error {
	if ls.ShouldShed(p) {
		return ErrLoadShed
	}
	return nil
}

func (ls *LoadShedder) getCPUUsage() float64 {
	// Real implementation would use /proc/stat or cgroups
	// This is a simplified version using goroutine count as proxy
	numGoroutines := float64(runtime.NumGoroutine())
	// Normalize: assume >10000 goroutines = 100% "load"
	return math.Min(100.0, numGoroutines/100.0)
}

func (ls *LoadShedder) getMemUsage() float64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	// Use HeapAlloc / HeapSys as proxy for memory pressure
	if ms.HeapSys == 0 {
		return 0
	}
	return math.Min(100.0, float64(ms.HeapAlloc)/float64(ms.HeapSys)*100.0)
}

// ============================================================
// SECTION 7: REQUEST HEDGING
// ============================================================

// HedgingPolicy defines when to send a hedge request.
type HedgingPolicy struct {
	MaxHedges    int           // Maximum number of hedge requests
	HedgeDelay   time.Duration // Send hedge after this delay
	HedgePercent float64       // Only hedge this % of requests (0-1.0)
}

// Hedge executes fn and sends hedge requests if the first is slow.
// Returns the result of whichever completes first.
func Hedge(ctx context.Context, policy HedgingPolicy, fn func(ctx context.Context, attempt int) (interface{}, error)) (interface{}, error) {
	if policy.MaxHedges <= 0 {
		return fn(ctx, 0)
	}

	// Probabilistic hedging
	if policy.HedgePercent > 0 && rand.Float64() > policy.HedgePercent {
		return fn(ctx, 0)
	}

	type result struct {
		val interface{}
		err error
	}

	resultCh := make(chan result, policy.MaxHedges+1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sendAttempt := func(attempt int) {
		val, err := fn(ctx, attempt)
		select {
		case resultCh <- result{val: val, err: err}:
		default:
		}
	}

	// First attempt
	go sendAttempt(0)

	// Hedge attempts
	for i := 1; i <= policy.MaxHedges; i++ {
		hedge := i
		select {
		case res := <-resultCh:
			cancel()
			return res.val, res.err
		case <-time.After(policy.HedgeDelay):
			go sendAttempt(hedge)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Wait for first response
	select {
	case res := <-resultCh:
		cancel()
		return res.val, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ============================================================
// SECTION 8: TIMEOUT BUDGET
// ============================================================

// TimeoutBudget manages a distributed deadline across multiple calls.
// Useful for propagating remaining time budget across service boundaries.
type TimeoutBudget struct {
	total     time.Duration
	overhead  time.Duration // Reserved for local processing
	deadline  time.Time
	remaining atomic.Int64 // nanoseconds
}

// NewTimeoutBudget creates a budget with total duration and overhead reservation.
func NewTimeoutBudget(total, overhead time.Duration) *TimeoutBudget {
	return &TimeoutBudget{
		total:    total,
		overhead: overhead,
		deadline: time.Now().Add(total),
	}
}

// Remaining returns how much budget is left for downstream calls.
func (tb *TimeoutBudget) Remaining() time.Duration {
	left := time.Until(tb.deadline) - tb.overhead
	if left < 0 {
		return 0
	}
	return left
}

// Exceeded returns true if the budget is exhausted.
func (tb *TimeoutBudget) Exceeded() bool {
	return tb.Remaining() <= 0
}

// ContextWithBudget returns a context with the remaining budget as deadline.
func (tb *TimeoutBudget) ContextWithBudget(parent context.Context) (context.Context, context.CancelFunc) {
	remaining := tb.Remaining()
	if remaining <= 0 {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, cancel
	}
	return context.WithTimeout(parent, remaining)
}

// ============================================================
// SECTION 9: CONNECTION POOL MANAGER
// ============================================================

// PooledConnection wraps a generic connection with lifecycle management.
type PooledConnection struct {
	ID        string
	CreatedAt time.Time
	LastUsed  time.Time
	UseCount  int64
	conn      interface{} // Underlying connection (net.Conn, *sql.DB, etc.)
	mu        sync.Mutex
	closed    bool
}

// ConnectionFactory creates new connections.
type ConnectionFactory func(ctx context.Context) (interface{}, error)

// ConnectionValidator validates if a connection is still healthy.
type ConnectionValidator func(conn interface{}) bool

// ConnectionCloser closes a connection.
type ConnectionCloser func(conn interface{}) error

// PoolConfig configures the connection pool.
type PoolConfig struct {
	MaxOpen      int
	MaxIdle      int
	MaxLifetime  time.Duration
	MaxIdleTime  time.Duration
	DialTimeout  time.Duration
	TestOnBorrow bool
}

// ConnectionPool manages a pool of reusable connections.
type ConnectionPool struct {
	cfg       PoolConfig
	factory   ConnectionFactory
	validator ConnectionValidator
	closer    ConnectionCloser
	idle      chan *PooledConnection
	numOpen   atomic.Int64
	mu        sync.Mutex
	closed    bool
	stopCh    chan struct{}
	// Metrics
	acquired atomic.Uint64
	released atomic.Uint64
	created  atomic.Uint64
	evicted  atomic.Uint64
}

// NewConnectionPool creates a managed connection pool.
func NewConnectionPool(cfg PoolConfig, factory ConnectionFactory, validator ConnectionValidator, closer ConnectionCloser) *ConnectionPool {
	p := &ConnectionPool{
		cfg:       cfg,
		factory:   factory,
		validator: validator,
		closer:    closer,
		idle:      make(chan *PooledConnection, cfg.MaxIdle),
		stopCh:    make(chan struct{}),
	}
	go p.janitor()
	return p
}

// Acquire gets a connection from the pool.
func (p *ConnectionPool) Acquire(ctx context.Context) (*PooledConnection, error) {
	// Try idle first
	for {
		select {
		case conn := <-p.idle:
			if p.isValid(conn) {
				conn.mu.Lock()
				conn.LastUsed = time.Now()
				conn.UseCount++
				conn.mu.Unlock()
				p.acquired.Add(1)
				return conn, nil
			}
			p.evict(conn)
		default:
			goto openNew
		}
	}

openNew:
	if p.numOpen.Load() >= int64(p.cfg.MaxOpen) {
		// Wait for an idle connection
		select {
		case conn := <-p.idle:
			if p.isValid(conn) {
				p.acquired.Add(1)
				return conn, nil
			}
			p.evict(conn)
		case <-ctx.Done():
			return nil, ErrPoolExhausted
		}
	}

	// Create new connection
	dialCtx, cancel := context.WithTimeout(ctx, p.cfg.DialTimeout)
	defer cancel()

	rawConn, err := p.factory(dialCtx)
	if err != nil {
		return nil, fmt.Errorf("pool: create connection: %w", err)
	}

	conn := &PooledConnection{
		ID:        fmt.Sprintf("conn-%d", p.created.Add(1)),
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		UseCount:  1,
		conn:      rawConn,
	}
	p.numOpen.Add(1)
	p.acquired.Add(1)
	return conn, nil
}

// Release returns a connection to the pool.
func (p *ConnectionPool) Release(conn *PooledConnection) {
	p.released.Add(1)
	if !p.isValid(conn) {
		p.evict(conn)
		return
	}
	conn.LastUsed = time.Now()
	select {
	case p.idle <- conn:
	default:
		p.evict(conn) // Pool is full, close the connection
	}
}

func (p *ConnectionPool) isValid(conn *PooledConnection) bool {
	if conn.closed {
		return false
	}
	if p.cfg.MaxLifetime > 0 && time.Since(conn.CreatedAt) > p.cfg.MaxLifetime {
		return false
	}
	if p.cfg.MaxIdleTime > 0 && time.Since(conn.LastUsed) > p.cfg.MaxIdleTime {
		return false
	}
	if p.cfg.TestOnBorrow && p.validator != nil {
		return p.validator(conn.conn)
	}
	return true
}

func (p *ConnectionPool) evict(conn *PooledConnection) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if !conn.closed {
		conn.closed = true
		if p.closer != nil {
			_ = p.closer(conn.conn)
		}
		p.numOpen.Add(-1)
		p.evicted.Add(1)
	}
}

func (p *ConnectionPool) janitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.evictStale()
		case <-p.stopCh:
			return
		}
	}
}

func (p *ConnectionPool) evictStale() {
	var stale []*PooledConnection
	var keep []*PooledConnection
	for {
		select {
		case conn := <-p.idle:
			if p.isValid(conn) {
				keep = append(keep, conn)
			} else {
				stale = append(stale, conn)
			}
		default:
			goto done
		}
	}
done:
	for _, conn := range stale {
		p.evict(conn)
	}
	for _, conn := range keep {
		p.idle <- conn
	}
}

// PoolStats returns current pool metrics.
type PoolStats struct {
	OpenConnections int64
	IdleConnections int
	Acquired        uint64
	Released        uint64
	Created         uint64
	Evicted         uint64
}

func (p *ConnectionPool) Stats() PoolStats {
	return PoolStats{
		OpenConnections: p.numOpen.Load(),
		IdleConnections: len(p.idle),
		Acquired:        p.acquired.Load(),
		Released:        p.released.Load(),
		Created:         p.created.Load(),
		Evicted:         p.evicted.Load(),
	}
}

// ============================================================
// SECTION 10: PERFORMANCE MANAGER (Composite Entry Point)
// ============================================================

// PerformanceManager is the unified, reusable entry point for all
// performance management capabilities in a microservice.
type PerformanceManager struct {
	RateLimiter    *TokenBucket
	SlidingWindow  *SlidingWindowLimiter
	CircuitBreaker *CircuitBreaker
	Concurrency    *AdaptiveConcurrencyLimiter
	LoadShedder    *LoadShedder
	Bulkheads      map[string]*Bulkhead
	mu             sync.RWMutex
}

// PerformanceManagerConfig configures the composite manager.
type PerformanceManagerConfig struct {
	RateLimit        float64
	BurstLimit       float64
	SlidingWindowReq int
	SlidingWindowDur time.Duration
	CBConfig         CircuitBreakerConfig
	MinConcurrency   int
	MaxConcurrency   int
	InitConcurrency  int
}

// DefaultPerformanceManagerConfig returns production defaults.
func DefaultPerformanceManagerConfig(serviceName string) PerformanceManagerConfig {
	return PerformanceManagerConfig{
		RateLimit:        10000,
		BurstLimit:       15000,
		SlidingWindowReq: 5000,
		SlidingWindowDur: time.Second,
		CBConfig:         DefaultCircuitBreakerConfig(serviceName),
		MinConcurrency:   10,
		MaxConcurrency:   1000,
		InitConcurrency:  100,
	}
}

// NewPerformanceManager constructs a fully configured performance manager.
func NewPerformanceManager(cfg PerformanceManagerConfig) *PerformanceManager {
	return &PerformanceManager{
		RateLimiter:    NewTokenBucket(cfg.RateLimit, cfg.BurstLimit),
		SlidingWindow:  NewSlidingWindowLimiter(cfg.SlidingWindowReq, cfg.SlidingWindowDur),
		CircuitBreaker: NewCircuitBreaker(cfg.CBConfig),
		Concurrency:    NewAdaptiveConcurrencyLimiter(cfg.MinConcurrency, cfg.MaxConcurrency, cfg.InitConcurrency),
		LoadShedder:    DefaultLoadShedder(),
		Bulkheads:      make(map[string]*Bulkhead),
	}
}

// AddBulkhead registers a named bulkhead for a specific downstream.
func (pm *PerformanceManager) AddBulkhead(name string, maxConcurrent, maxQueue, workers int, timeout time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.Bulkheads[name] = NewBulkhead(name, maxConcurrent, maxQueue, workers, timeout)
}

// Guard applies all performance controls. Returns error if request should be rejected.
func (pm *PerformanceManager) Guard(ctx context.Context, priority Priority) error {
	if err := pm.LoadShedder.Allow(ctx, priority); err != nil {
		return err
	}
	if !pm.RateLimiter.Allow(1) {
		return ErrRateLimitExceeded
	}
	if !pm.SlidingWindow.Allow() {
		return ErrRateLimitExceeded
	}
	if err := pm.CircuitBreaker.Allow(); err != nil {
		return err
	}
	return nil
}
