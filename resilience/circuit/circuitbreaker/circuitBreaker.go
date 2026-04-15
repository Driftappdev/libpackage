// Package gocircuit implements the Circuit Breaker pattern with half-open
// probing, per-key breakers, metrics hooks, and event subscriptions.
package gocircuit

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ---- States -----------------------------------------------------------------

// State represents the current state of a circuit breaker.
type State int32

const (
	StateClosed   State = iota // normal operation
	StateHalfOpen              // probing — limited traffic allowed
	StateOpen                  // rejecting all requests
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateHalfOpen:
		return "HALF_OPEN"
	case StateOpen:
		return "OPEN"
	}
	return "UNKNOWN"
}

// ---- Errors -----------------------------------------------------------------

// ErrOpen is returned when the circuit is open and a call is rejected.
var ErrOpen = errors.New("gocircuit: circuit is OPEN — request rejected")

// ---- Events -----------------------------------------------------------------

// EventType identifies a circuit breaker state-change event.
type EventType string

const (
	EventOpened     EventType = "OPENED"
	EventClosed     EventType = "CLOSED"
	EventHalfOpened EventType = "HALF_OPENED"
	EventSuccess    EventType = "SUCCESS"
	EventFailure    EventType = "FAILURE"
)

// Event is emitted on state transitions or call outcomes.
type Event struct {
	Name      string
	Type      EventType
	State     State
	Timestamp time.Time
	Err       error // non-nil on failure events
}

// ---- Rolling window ---------------------------------------------------------

// bucket holds counters for one time-bucket inside the rolling window.
type bucket struct {
	successes int64
	failures  int64
	timeouts  int64
}

// rollingWindow maintains counters over a sliding time window divided into slots.
type rollingWindow struct {
	mu       sync.Mutex
	buckets  []bucket
	size     int
	interval time.Duration
	lastTick time.Time
}

func newRollingWindow(size int, windowDuration time.Duration) *rollingWindow {
	return &rollingWindow{
		buckets:  make([]bucket, size),
		size:     size,
		interval: windowDuration / time.Duration(size),
		lastTick: time.Now(),
	}
}

func (rw *rollingWindow) advance(now time.Time) {
	elapsed := now.Sub(rw.lastTick)
	ticks := int(elapsed / rw.interval)
	if ticks == 0 {
		return
	}
	rw.lastTick = rw.lastTick.Add(time.Duration(ticks) * rw.interval)
	clear := ticks
	if clear > rw.size {
		clear = rw.size
	}
	// Shift and zero out stale buckets.
	rw.buckets = append(make([]bucket, clear), rw.buckets[:rw.size-clear]...)
}

func (rw *rollingWindow) record(success bool, timeout bool) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.advance(time.Now())
	if success {
		rw.buckets[0].successes++
	} else if timeout {
		rw.buckets[0].timeouts++
		rw.buckets[0].failures++
	} else {
		rw.buckets[0].failures++
	}
}

func (rw *rollingWindow) totals() (successes, failures, total int64) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.advance(time.Now())
	for _, b := range rw.buckets {
		successes += b.successes
		failures += b.failures
	}
	total = successes + failures
	return
}

// ---- Config -----------------------------------------------------------------

// Config holds circuit breaker parameters.
type Config struct {
	// Name identifies the breaker in logs and events.
	Name string

	// FailureThreshold is the percentage (0–100) of failures that trips the breaker.
	FailureThreshold float64

	// MinimumRequestVolume is the minimum number of requests needed before the
	// failure rate is evaluated.
	MinimumRequestVolume int64

	// WindowSize is the number of buckets in the rolling window.
	WindowSize int

	// WindowDuration is the total duration of the rolling window.
	WindowDuration time.Duration

	// OpenTimeout is how long the breaker stays OPEN before probing.
	OpenTimeout time.Duration

	// HalfOpenMaxCalls is the number of probes allowed in HALF_OPEN state.
	HalfOpenMaxCalls int64

	// OnEvent is called on every state transition or call outcome.
	OnEvent func(Event)

	// IsSuccessful overrides what counts as success. Default: err == nil.
	IsSuccessful func(err error) bool
}

// DefaultConfig returns a production-suitable default configuration.
func DefaultConfig(name string) Config {
	return Config{
		Name:                 name,
		FailureThreshold:     50.0,
		MinimumRequestVolume: 20,
		WindowSize:           10,
		WindowDuration:       10 * time.Second,
		OpenTimeout:          5 * time.Second,
		HalfOpenMaxCalls:     5,
	}
}

// ---- Breaker ----------------------------------------------------------------

// Breaker is a single circuit breaker instance.
type Breaker struct {
	cfg    Config
	mu     sync.Mutex
	state  atomic.Int32
	window *rollingWindow

	openedAt       time.Time
	halfOpenCalls  atomic.Int64
	consecutiveOK  atomic.Int64
}

// New creates a new Breaker with the given configuration.
func New(cfg Config) *Breaker {
	b := &Breaker{
		cfg:    cfg,
		window: newRollingWindow(cfg.WindowSize, cfg.WindowDuration),
	}
	b.state.Store(int32(StateClosed))
	return b
}

// State returns the current state of the breaker.
func (b *Breaker) State() State { return State(b.state.Load()) }

// Allow checks whether a call should be allowed through.
// Returns ErrOpen if the circuit is open.
func (b *Breaker) Allow() error {
	state := b.State()
	switch state {
	case StateClosed:
		return nil
	case StateOpen:
		b.mu.Lock()
		if time.Since(b.openedAt) >= b.cfg.OpenTimeout {
			b.transition(StateHalfOpen)
			b.mu.Unlock()
			return nil
		}
		b.mu.Unlock()
		return fmt.Errorf("%w (%s)", ErrOpen, b.cfg.Name)
	case StateHalfOpen:
		if b.halfOpenCalls.Add(1) <= b.cfg.HalfOpenMaxCalls {
			return nil
		}
		b.halfOpenCalls.Add(-1)
		return fmt.Errorf("%w (%s): half-open capacity exceeded", ErrOpen, b.cfg.Name)
	}
	return nil
}

// RecordSuccess records a successful call.
func (b *Breaker) RecordSuccess() {
	b.window.record(true, false)
	b.emit(Event{Name: b.cfg.Name, Type: EventSuccess, State: b.State(), Timestamp: time.Now()})

	if b.State() == StateHalfOpen {
		if b.consecutiveOK.Add(1) >= b.cfg.HalfOpenMaxCalls {
			b.mu.Lock()
			b.transition(StateClosed)
			b.mu.Unlock()
		}
	}
}

// RecordFailure records a failed call.
func (b *Breaker) RecordFailure(err error) {
	b.window.record(false, false)
	b.emit(Event{Name: b.cfg.Name, Type: EventFailure, State: b.State(), Timestamp: time.Now(), Err: err})
	b.consecutiveOK.Store(0)

	if b.State() == StateHalfOpen {
		b.mu.Lock()
		b.transition(StateOpen)
		b.mu.Unlock()
		return
	}

	_, failures, total := b.window.totals()
	minVol := b.cfg.MinimumRequestVolume
	if minVol == 0 {
		minVol = 20
	}
	thresh := b.cfg.FailureThreshold
	if thresh == 0 {
		thresh = 50
	}

	if total >= minVol {
		rate := float64(failures) / float64(total) * 100
		if rate >= thresh {
			b.mu.Lock()
			if b.State() == StateClosed {
				b.transition(StateOpen)
			}
			b.mu.Unlock()
		}
	}
}

// Do is the all-in-one convenience method: checks Allow, runs fn, records outcome.
func (b *Breaker) Do(fn func() error) error {
	if err := b.Allow(); err != nil {
		return err
	}
	err := fn()
	isOK := err == nil
	if b.cfg.IsSuccessful != nil {
		isOK = b.cfg.IsSuccessful(err)
	}
	if isOK {
		b.RecordSuccess()
	} else {
		b.RecordFailure(err)
	}
	return err
}

// Stats returns current window statistics.
func (b *Breaker) Stats() (successes, failures, total int64, failureRate float64) {
	successes, failures, total = b.window.totals()
	if total > 0 {
		failureRate = float64(failures) / float64(total) * 100
	}
	return
}

// Reset forcibly moves the breaker to CLOSED state and clears counters.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.window = newRollingWindow(b.cfg.WindowSize, b.cfg.WindowDuration)
	b.consecutiveOK.Store(0)
	b.halfOpenCalls.Store(0)
	b.transition(StateClosed)
}

func (b *Breaker) transition(next State) {
	prev := State(b.state.Swap(int32(next)))
	if prev == next {
		return
	}
	if next == StateOpen {
		b.openedAt = time.Now()
		b.halfOpenCalls.Store(0)
	}
	if next == StateClosed {
		b.consecutiveOK.Store(0)
	}
	et := EventClosed
	switch next {
	case StateOpen:
		et = EventOpened
	case StateHalfOpen:
		et = EventHalfOpened
	}
	b.emit(Event{Name: b.cfg.Name, Type: et, State: next, Timestamp: time.Now()})
}

func (b *Breaker) emit(e Event) {
	if b.cfg.OnEvent != nil {
		b.cfg.OnEvent(e)
	}
}

// ---- Registry ---------------------------------------------------------------

// Registry manages multiple named circuit breakers.
type Registry struct {
	mu       sync.RWMutex
	breakers map[string]*Breaker
	defaults Config
}

// NewRegistry creates a Registry with the given default config template.
func NewRegistry(defaults Config) *Registry {
	return &Registry{breakers: make(map[string]*Breaker), defaults: defaults}
}

// Get returns the breaker with the given name, creating it if necessary.
func (r *Registry) Get(name string) *Breaker {
	r.mu.RLock()
	b, ok := r.breakers[name]
	r.mu.RUnlock()
	if ok {
		return b
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok = r.breakers[name]; ok {
		return b
	}
	cfg := r.defaults
	cfg.Name = name
	b = New(cfg)
	r.breakers[name] = b
	return b
}

// All returns all registered breakers.
func (r *Registry) All() map[string]*Breaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*Breaker, len(r.breakers))
	for k, v := range r.breakers {
		out[k] = v
	}
	return out
}
