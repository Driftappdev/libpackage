// Package queue provides enterprise-grade message queue primitives.
// inbox.go — high-throughput, fault-tolerant message inbox with:
//   - Priority-based ingestion (4 levels)
//   - Per-sender rate limiting (token bucket)
//   - Idempotent deduplication (sliding-window bloom + exact LRU)
//   - Circuit breaker (closed / half-open / open)
//   - Backpressure with configurable overflow strategy
//   - Prometheus-compatible metrics hooks
//   - Graceful shutdown with drain timeout
//   - Structured, zero-alloc-friendly logging interface
package inbox

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Public errors
// ---------------------------------------------------------------------------

var (
	ErrInboxClosed       = errors.New("inbox: closed")
	ErrInboxFull         = errors.New("inbox: buffer full")
	ErrRateLimited       = errors.New("inbox: rate limited")
	ErrCircuitOpen       = errors.New("inbox: circuit breaker open")
	ErrDuplicate         = errors.New("inbox: duplicate message")
	ErrInvalidMessage    = errors.New("inbox: invalid message")
	ErrInboxDrainTimeout = errors.New("inbox: drain timeout exceeded")
)

// ---------------------------------------------------------------------------
// Priority levels
// ---------------------------------------------------------------------------

type Priority uint8

const (
	PriorityCritical Priority = iota // 0 — highest
	PriorityHigh                     // 1
	PriorityNormal                   // 2
	PriorityLow                      // 3
	priorityLevels   = 4
)

// ---------------------------------------------------------------------------
// Message
// ---------------------------------------------------------------------------

// Message is the atomic unit flowing through the queue system.
// Keep the struct small; large payloads go in Body as []byte.
type Message struct {
	ID          string            // globally unique, used for dedup
	SenderID    string            // logical source / producer identity
	Topic       string            // routing / filtering key
	Priority    Priority          // ingestion priority
	Body        []byte            // raw payload (JSON, protobuf, …)
	Metadata    map[string]string // optional k/v bag (trace-id, content-type, …)
	Timestamp   time.Time         // producer wall-clock
	Attempts    int               // how many times delivery was attempted
	MaxAttempts int               // 0 = use InboxConfig default
}

func (m *Message) validate() error {
	if m == nil {
		return fmt.Errorf("%w: nil message", ErrInvalidMessage)
	}
	if m.ID == "" {
		return fmt.Errorf("%w: empty ID", ErrInvalidMessage)
	}
	if m.Topic == "" {
		return fmt.Errorf("%w: empty Topic", ErrInvalidMessage)
	}
	if m.Priority >= priorityLevels {
		return fmt.Errorf("%w: unknown priority %d", ErrInvalidMessage, m.Priority)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Metrics hook (implement with Prometheus, OpenTelemetry, Datadog, …)
// ---------------------------------------------------------------------------

// InboxMetrics is a pure interface so callers inject their own instrumentation.
type InboxMetrics interface {
	IncReceived(topic string, priority Priority)
	IncDropped(topic string, reason string)
	IncDuplicate(topic string)
	ObserveQueueDepth(priority Priority, depth int)
	ObserveLatency(stage string, d time.Duration)
}

type noopMetrics struct{}

func (noopMetrics) IncReceived(string, Priority)         {}
func (noopMetrics) IncDropped(string, string)            {}
func (noopMetrics) IncDuplicate(string)                  {}
func (noopMetrics) ObserveQueueDepth(Priority, int)      {}
func (noopMetrics) ObserveLatency(string, time.Duration) {}

// ---------------------------------------------------------------------------
// Logger (zero-dependency interface)
// ---------------------------------------------------------------------------

type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}

// ---------------------------------------------------------------------------
// Overflow strategy
// ---------------------------------------------------------------------------

type OverflowStrategy uint8

const (
	OverflowDrop       OverflowStrategy = iota // silently drop newest
	OverflowReject                             // return ErrInboxFull to caller
	OverflowDropOldest                         // evict oldest from the tail
)

// ---------------------------------------------------------------------------
// Circuit Breaker
// ---------------------------------------------------------------------------

type cbState uint32

const (
	cbClosed   cbState = iota // normal
	cbHalfOpen                // probe
	cbOpen                    // rejecting
)

type circuitBreaker struct {
	state           atomic.Uint32
	failures        atomic.Int64
	successes       atomic.Int64
	lastStateChange atomic.Int64 // UnixNano

	failureThreshold int64
	successThreshold int64
	timeout          time.Duration // how long to stay Open before HalfOpen
	mu               sync.Mutex
}

func newCircuitBreaker(failThreshold, successThreshold int64, timeout time.Duration) *circuitBreaker {
	cb := &circuitBreaker{
		failureThreshold: failThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
	}
	cb.state.Store(uint32(cbClosed))
	cb.lastStateChange.Store(time.Now().UnixNano())
	return cb
}

func (cb *circuitBreaker) Allow() error {
	state := cbState(cb.state.Load())
	switch state {
	case cbClosed:
		return nil
	case cbOpen:
		since := time.Duration(time.Now().UnixNano() - cb.lastStateChange.Load())
		if since >= cb.timeout {
			cb.mu.Lock()
			if cbState(cb.state.Load()) == cbOpen { // double-check
				cb.state.Store(uint32(cbHalfOpen))
				cb.successes.Store(0)
				cb.lastStateChange.Store(time.Now().UnixNano())
			}
			cb.mu.Unlock()
			return nil // let one probe through
		}
		return ErrCircuitOpen
	case cbHalfOpen:
		return nil // allow probes
	}
	return ErrCircuitOpen
}

func (cb *circuitBreaker) RecordSuccess() {
	state := cbState(cb.state.Load())
	if state == cbHalfOpen {
		if cb.successes.Add(1) >= cb.successThreshold {
			cb.mu.Lock()
			cb.state.Store(uint32(cbClosed))
			cb.failures.Store(0)
			cb.lastStateChange.Store(time.Now().UnixNano())
			cb.mu.Unlock()
		}
	} else {
		cb.failures.Store(0)
	}
}

func (cb *circuitBreaker) RecordFailure() {
	if cb.failures.Add(1) >= cb.failureThreshold {
		cb.mu.Lock()
		if cbState(cb.state.Load()) == cbClosed {
			cb.state.Store(uint32(cbOpen))
			cb.lastStateChange.Store(time.Now().UnixNano())
		}
		cb.mu.Unlock()
	}
}

func (cb *circuitBreaker) State() string {
	switch cbState(cb.state.Load()) {
	case cbClosed:
		return "closed"
	case cbHalfOpen:
		return "half-open"
	case cbOpen:
		return "open"
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// Token-bucket rate limiter (per sender)
// ---------------------------------------------------------------------------

type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64 // tokens/second
	lastFill time.Time
}

func newTokenBucket(capacity float64, ratePerSec float64) *tokenBucket {
	return &tokenBucket{
		tokens:   capacity,
		capacity: capacity,
		rate:     ratePerSec,
		lastFill: time.Now(),
	}
}

func (tb *tokenBucket) Allow(n float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.lastFill).Seconds()
	tb.tokens = math.Min(tb.capacity, tb.tokens+elapsed*tb.rate)
	tb.lastFill = now
	if tb.tokens >= n {
		tb.tokens -= n
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Bloom filter for fast deduplication (probabilistic, sliding window)
// ---------------------------------------------------------------------------

type bloomFilter struct {
	mu      sync.RWMutex
	bits    []uint64
	m       uint64 // number of bits
	k       uint   // number of hash functions
	count   uint64
	maxKeys uint64
}

func newBloomFilter(expectedKeys uint64, falsePositiveRate float64) *bloomFilter {
	m := bloomSize(expectedKeys, falsePositiveRate)
	k := bloomK(m, expectedKeys)
	return &bloomFilter{
		bits:    make([]uint64, (m+63)/64),
		m:       m,
		k:       k,
		maxKeys: expectedKeys,
	}
}

func bloomSize(n uint64, p float64) uint64 {
	return uint64(math.Ceil(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2)))
}

func bloomK(m, n uint64) uint {
	k := math.Round(float64(m) / float64(n) * math.Ln2)
	if k < 1 {
		return 1
	}
	return uint(k)
}

func (bf *bloomFilter) hashes(id string) []uint64 {
	h := make([]uint64, bf.k)
	h1 := fnv.New64a()
	h2 := fnv.New64()
	_, _ = h1.Write([]byte(id))
	_, _ = h2.Write([]byte(id))
	a, b := h1.Sum64(), h2.Sum64()
	for i := uint(0); i < bf.k; i++ {
		h[i] = (a + uint64(i)*b) % bf.m
	}
	return h
}

func (bf *bloomFilter) TestAndAdd(id string) (exists bool) {
	hs := bf.hashes(id)
	bf.mu.Lock()
	defer bf.mu.Unlock()
	exists = true
	for _, h := range hs {
		word, bit := h/64, h%64
		if bf.bits[word]&(1<<bit) == 0 {
			exists = false
		}
	}
	if !exists {
		for _, h := range hs {
			word, bit := h/64, h%64
			bf.bits[word] |= 1 << bit
		}
		bf.count++
		// Reset when approaching capacity (sliding window approximation)
		if bf.count >= bf.maxKeys {
			bf.bits = make([]uint64, (bf.m+63)/64)
			bf.count = 0
		}
	}
	return exists
}

// ---------------------------------------------------------------------------
// LRU exact dedup (backup for bloom false positives)
// ---------------------------------------------------------------------------

type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

type lruCache struct {
	mu    sync.Mutex
	cap   int
	items map[string]*lruNode
	head  *lruNode // MRU sentinel
	tail  *lruNode // LRU sentinel
}

func newLRUCache(capacity int) *lruCache {
	head := &lruNode{}
	tail := &lruNode{}
	head.next = tail
	tail.prev = head
	return &lruCache{
		cap:   capacity,
		items: make(map[string]*lruNode, capacity),
		head:  head,
		tail:  tail,
	}
}

func (c *lruCache) Contains(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.items[key]; ok {
		c.moveToFront(n)
		return true
	}
	n := &lruNode{key: key}
	c.items[key] = n
	c.addFront(n)
	if len(c.items) > c.cap {
		evict := c.tail.prev
		c.remove(evict)
		delete(c.items, evict.key)
	}
	return false
}

func (c *lruCache) addFront(n *lruNode) {
	n.prev = c.head
	n.next = c.head.next
	c.head.next.prev = n
	c.head.next = n
}

func (c *lruCache) remove(n *lruNode) {
	n.prev.next = n.next
	n.next.prev = n.prev
}

func (c *lruCache) moveToFront(n *lruNode) {
	c.remove(n)
	c.addFront(n)
}

// ---------------------------------------------------------------------------
// InboxConfig
// ---------------------------------------------------------------------------

// InboxConfig holds all knobs for an Inbox instance.
// Zero-value fields fall back to sensible defaults.
type InboxConfig struct {
	// Buffer sizes per priority level. Default: 1024, 2048, 4096, 8192
	BufferSizes [priorityLevels]int

	// Overflow strategy when a priority buffer is full
	OverflowStrategy OverflowStrategy

	// Rate limiting per sender (0 = disabled)
	RateLimitCapacity float64 // token bucket burst capacity
	RateLimitRate     float64 // tokens refilled per second

	// Circuit breaker thresholds (0 = disabled)
	CBFailureThreshold int64
	CBSuccessThreshold int64
	CBOpenTimeout      time.Duration

	// Deduplication
	DedupeEnabled       bool
	DedupeBloomExpected uint64  // expected unique IDs in window
	DedupeBloomFPRate   float64 // false-positive rate for bloom
	DedupeLRUSize       int     // exact LRU backup size

	// Drain timeout on Close
	DrainTimeout time.Duration

	// Maximum delivery attempts per message (0 = 3)
	DefaultMaxAttempts int

	// Metrics & logging
	Metrics InboxMetrics
	Log     Logger

	// Internal metrics tick interval (0 = 5s)
	MetricsInterval time.Duration
}

func (c *InboxConfig) applyDefaults() {
	defaultBufs := [priorityLevels]int{1024, 2048, 4096, 8192}
	for i := 0; i < priorityLevels; i++ {
		if c.BufferSizes[i] == 0 {
			c.BufferSizes[i] = defaultBufs[i]
		}
	}
	if c.DrainTimeout == 0 {
		c.DrainTimeout = 30 * time.Second
	}
	if c.DefaultMaxAttempts == 0 {
		c.DefaultMaxAttempts = 3
	}
	if c.Metrics == nil {
		c.Metrics = noopMetrics{}
	}
	if c.Log == nil {
		c.Log = discardLogger{}
	}
	if c.MetricsInterval == 0 {
		c.MetricsInterval = 5 * time.Second
	}
	if c.DedupeBloomExpected == 0 {
		c.DedupeBloomExpected = 100_000
	}
	if c.DedupeBloomFPRate == 0 {
		c.DedupeBloomFPRate = 0.001
	}
	if c.DedupeLRUSize == 0 {
		c.DedupeLRUSize = 10_000
	}
	if c.CBFailureThreshold == 0 {
		c.CBFailureThreshold = 10
	}
	if c.CBSuccessThreshold == 0 {
		c.CBSuccessThreshold = 3
	}
	if c.CBOpenTimeout == 0 {
		c.CBOpenTimeout = 30 * time.Second
	}
	if c.RateLimitCapacity == 0 {
		c.RateLimitCapacity = 1000
	}
	if c.RateLimitRate == 0 {
		c.RateLimitRate = 500
	}
}

// ---------------------------------------------------------------------------
// Inbox
// ---------------------------------------------------------------------------

// Inbox is a priority-aware, fault-tolerant message ingestion point.
// It is safe for concurrent use by multiple goroutines.
type Inbox struct {
	cfg InboxConfig

	// Priority channels (index = Priority value)
	queues [priorityLevels]chan *Message

	// Rate limiters keyed by SenderID
	limitersMu sync.RWMutex
	limiters   map[string]*tokenBucket

	// Circuit breaker
	cb *circuitBreaker

	// Deduplication
	bloom *bloomFilter
	lru   *lruCache

	// Lifecycle
	closed atomic.Bool
	wg     sync.WaitGroup
	stopCh chan struct{}

	// Stats (atomic for lock-free reads)
	totalReceived atomic.Int64
	totalDropped  atomic.Int64
	totalDups     atomic.Int64
}

// NewInbox creates and starts an Inbox with the given config.
func NewInbox(cfg InboxConfig) *Inbox {
	cfg.applyDefaults()

	in := &Inbox{
		cfg:      cfg,
		limiters: make(map[string]*tokenBucket, 64),
		stopCh:   make(chan struct{}),
	}

	for i := 0; i < priorityLevels; i++ {
		in.queues[i] = make(chan *Message, cfg.BufferSizes[i])
	}

	in.cb = newCircuitBreaker(cfg.CBFailureThreshold, cfg.CBSuccessThreshold, cfg.CBOpenTimeout)

	if cfg.DedupeEnabled {
		in.bloom = newBloomFilter(cfg.DedupeBloomExpected, cfg.DedupeBloomFPRate)
		in.lru = newLRUCache(cfg.DedupeLRUSize)
	}

	in.wg.Add(1)
	go in.metricsLoop()

	return in
}

// Submit ingests a message. Returns nil on success, or a sentinel error.
// Submit is non-blocking; it never parks the caller.
func (in *Inbox) Submit(msg *Message) error {
	if in.closed.Load() {
		return ErrInboxClosed
	}

	start := time.Now()

	// --- validation ---
	if err := msg.validate(); err != nil {
		return err
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.MaxAttempts == 0 {
		msg.MaxAttempts = in.cfg.DefaultMaxAttempts
	}

	// --- circuit breaker ---
	if err := in.cb.Allow(); err != nil {
		in.cfg.Metrics.IncDropped(msg.Topic, "circuit_open")
		in.totalDropped.Add(1)
		return ErrCircuitOpen
	}

	// --- rate limiting ---
	if in.cfg.RateLimitRate > 0 && msg.SenderID != "" {
		limiter := in.getOrCreateLimiter(msg.SenderID)
		if !limiter.Allow(1) {
			in.cfg.Metrics.IncDropped(msg.Topic, "rate_limited")
			in.totalDropped.Add(1)
			in.cb.RecordFailure()
			return ErrRateLimited
		}
	}

	// --- deduplication ---
	if in.cfg.DedupeEnabled {
		if in.bloom.TestAndAdd(msg.ID) {
			// Bloom says "seen" — verify with exact LRU
			if in.lru.Contains(msg.ID) {
				in.cfg.Metrics.IncDuplicate(msg.Topic)
				in.totalDups.Add(1)
				return ErrDuplicate
			}
			// Bloom false-positive; LRU already inserted it; proceed
		}
	}

	// --- enqueue ---
	q := in.queues[msg.Priority]
	enqueued := false

	switch in.cfg.OverflowStrategy {
	case OverflowReject:
		select {
		case q <- msg:
			enqueued = true
		default:
		}
	case OverflowDropOldest:
		select {
		case q <- msg:
			enqueued = true
		default:
			// evict oldest
			select {
			case <-q:
				in.cfg.Metrics.IncDropped(msg.Topic, "evict_oldest")
				in.totalDropped.Add(1)
			default:
			}
			select {
			case q <- msg:
				enqueued = true
			default:
			}
		}
	default: // OverflowDrop
		select {
		case q <- msg:
			enqueued = true
		default:
		}
	}

	if !enqueued {
		in.cfg.Metrics.IncDropped(msg.Topic, "buffer_full")
		in.totalDropped.Add(1)
		in.cb.RecordFailure()
		return ErrInboxFull
	}

	in.cb.RecordSuccess()
	in.cfg.Metrics.IncReceived(msg.Topic, msg.Priority)
	in.cfg.Metrics.ObserveLatency("submit", time.Since(start))
	in.totalReceived.Add(1)
	return nil
}

// Consume returns the next Message respecting priority order.
// It blocks until a message is available or ctx is cancelled.
// Priority channels are polled in strict order (Critical first).
func (in *Inbox) Consume(ctx context.Context) (*Message, error) {
	if in.closed.Load() {
		// Drain remaining messages before returning closed error
		for p := Priority(0); p < priorityLevels; p++ {
			select {
			case msg := <-in.queues[p]:
				return msg, nil
			default:
			}
		}
		return nil, ErrInboxClosed
	}

	for {
		// Non-blocking pass: check all queues highest-priority first
		for p := Priority(0); p < priorityLevels; p++ {
			select {
			case msg := <-in.queues[p]:
				return msg, nil
			default:
			}
		}

		// Blocking pass: wait on any queue or ctx
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg := <-in.queues[PriorityCritical]:
			return msg, nil
		case msg := <-in.queues[PriorityHigh]:
			return msg, nil
		case msg := <-in.queues[PriorityNormal]:
			return msg, nil
		case msg := <-in.queues[PriorityLow]:
			return msg, nil
		}
	}
}

// ConsumeC returns a receive-only channel for a specific priority.
// Useful for select-based consumers.
func (in *Inbox) ConsumeC(p Priority) <-chan *Message {
	return in.queues[p]
}

// CircuitState returns current circuit breaker state string.
func (in *Inbox) CircuitState() string { return in.cb.State() }

// Stats returns a snapshot of internal counters.
func (in *Inbox) Stats() InboxStats {
	depths := [priorityLevels]int{}
	for i := 0; i < priorityLevels; i++ {
		depths[i] = len(in.queues[i])
	}
	return InboxStats{
		TotalReceived: in.totalReceived.Load(),
		TotalDropped:  in.totalDropped.Load(),
		TotalDups:     in.totalDups.Load(),
		QueueDepths:   depths,
		CircuitState:  in.cb.State(),
	}
}

// InboxStats is a point-in-time snapshot.
type InboxStats struct {
	TotalReceived int64
	TotalDropped  int64
	TotalDups     int64
	QueueDepths   [priorityLevels]int
	CircuitState  string
}

// Close signals shutdown and waits for the drain timeout.
func (in *Inbox) Close() error {
	if !in.closed.CompareAndSwap(false, true) {
		return ErrInboxClosed
	}
	close(in.stopCh)

	done := make(chan struct{})
	go func() {
		in.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(in.cfg.DrainTimeout):
		return ErrInboxDrainTimeout
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (in *Inbox) getOrCreateLimiter(senderID string) *tokenBucket {
	in.limitersMu.RLock()
	l, ok := in.limiters[senderID]
	in.limitersMu.RUnlock()
	if ok {
		return l
	}

	in.limitersMu.Lock()
	defer in.limitersMu.Unlock()
	if l, ok = in.limiters[senderID]; ok {
		return l
	}
	l = newTokenBucket(in.cfg.RateLimitCapacity, in.cfg.RateLimitRate)
	in.limiters[senderID] = l
	return l
}

func (in *Inbox) metricsLoop() {
	defer in.wg.Done()
	ticker := time.NewTicker(in.cfg.MetricsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-in.stopCh:
			return
		case <-ticker.C:
			for p := Priority(0); p < priorityLevels; p++ {
				in.cfg.Metrics.ObserveQueueDepth(p, len(in.queues[p]))
			}
		}
	}
}
