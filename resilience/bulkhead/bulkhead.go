package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BulkheadConfig configures a bulkhead isolation unit.
type BulkheadConfig struct {
	// Name identifies this bulkhead in metrics/logs.
	Name string
	// MaxConcurrent is the max in-flight calls for this partition.
	MaxConcurrent int
	// MaxQueue is the max waiting callers (0 = reject immediately when full).
	MaxQueue int
	// AcquireTimeout is how long a caller may wait for a slot.
	AcquireTimeout time.Duration
	// OnRejected is called when a call is rejected (circuit open or queue full).
	OnRejected func(name string, reason string)
	// OnAcquired is called when a slot is successfully acquired.
	OnAcquired func(name string, waited time.Duration)
	// OnReleased is called when a slot is released.
	OnReleased func(name string, duration time.Duration)
}

// Bulkhead isolates a resource partition using a bounded semaphore and
// optional queue. Inspired by Netflix Hystrix / Resilience4j bulkhead.
// Prevents one slow downstream from exhausting the entire goroutine pool.
type Bulkhead struct {
	cfg      BulkheadConfig
	sem      chan struct{}
	queue    atomic.Int64
	inflight atomic.Int64
	rejected atomic.Int64
	total    atomic.Int64
	mu       sync.RWMutex
	closed   bool
}

// NewBulkhead creates a new Bulkhead. Panics on invalid config.
func NewBulkhead(cfg BulkheadConfig) *Bulkhead {
	if cfg.MaxConcurrent < 1 {
		panic("resilience: Bulkhead MaxConcurrent must be >= 1")
	}
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	b := &Bulkhead{cfg: cfg, sem: make(chan struct{}, cfg.MaxConcurrent)}
	for i := 0; i < cfg.MaxConcurrent; i++ {
		b.sem <- struct{}{}
	}
	return b
}

// Acquire enters the bulkhead. Returns a token that MUST be released.
// Returns an error if the bulkhead is full, queue is full, or timeout occurs.
func (b *Bulkhead) Acquire(ctx context.Context) (*BulkheadToken, error) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return nil, fmt.Errorf("resilience: bulkhead[%s] is closed", b.cfg.Name)
	}
	b.mu.RUnlock()

	start := time.Now()

	// Fast path
	select {
	case <-b.sem:
		b.inflight.Add(1)
		b.total.Add(1)
		if b.cfg.OnAcquired != nil {
			b.cfg.OnAcquired(b.cfg.Name, time.Since(start))
		}
		return &BulkheadToken{b: b, start: start}, nil
	default:
	}

	// Check queue capacity
	maxQ := b.cfg.MaxQueue
	if maxQ > 0 && int(b.queue.Load()) >= maxQ {
		b.rejected.Add(1)
		reason := fmt.Sprintf("queue full (%d/%d)", b.queue.Load(), maxQ)
		if b.cfg.OnRejected != nil {
			b.cfg.OnRejected(b.cfg.Name, reason)
		}
		return nil, fmt.Errorf("resilience: bulkhead[%s] %s", b.cfg.Name, reason)
	}

	b.queue.Add(1)
	defer b.queue.Add(-1)

	waitCtx := ctx
	var cancel context.CancelFunc
	if b.cfg.AcquireTimeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, b.cfg.AcquireTimeout)
		defer cancel()
	}

	select {
	case <-b.sem:
		waited := time.Since(start)
		b.inflight.Add(1)
		b.total.Add(1)
		if b.cfg.OnAcquired != nil {
			b.cfg.OnAcquired(b.cfg.Name, waited)
		}
		return &BulkheadToken{b: b, start: start}, nil

	case <-waitCtx.Done():
		b.rejected.Add(1)
		reason := "timeout or cancelled"
		if ctx.Err() != nil {
			reason = "context cancelled"
		}
		if b.cfg.OnRejected != nil {
			b.cfg.OnRejected(b.cfg.Name, reason)
		}
		return nil, fmt.Errorf("resilience: bulkhead[%s] %s after %s",
			b.cfg.Name, reason, time.Since(start))
	}
}

// Execute runs fn within the bulkhead, acquiring and releasing automatically.
func (b *Bulkhead) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	token, err := b.Acquire(ctx)
	if err != nil {
		return err
	}
	defer token.Release()
	return fn(ctx)
}

// Close permanently shuts down the bulkhead. Subsequent Acquire calls fail.
func (b *Bulkhead) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
}

// Stats returns a snapshot of bulkhead counters.
func (b *Bulkhead) Stats() BulkheadStats {
	return BulkheadStats{
		Name:      b.cfg.Name,
		InFlight:  b.inflight.Load(),
		Queued:    b.queue.Load(),
		Rejected:  b.rejected.Load(),
		Total:     b.total.Load(),
		Capacity:  int64(b.cfg.MaxConcurrent),
		Available: int64(len(b.sem)),
	}
}

// BulkheadStats is a point-in-time snapshot.
type BulkheadStats struct {
	Name      string
	InFlight  int64
	Queued    int64
	Rejected  int64
	Total     int64
	Capacity  int64
	Available int64
}

// BulkheadToken represents an acquired slot in the bulkhead.
// Release MUST be called exactly once.
type BulkheadToken struct {
	b     *Bulkhead
	start time.Time
	once  sync.Once
}

// Release returns the slot to the bulkhead.
func (t *BulkheadToken) Release() {
	t.once.Do(func() {
		dur := time.Since(t.start)
		t.b.inflight.Add(-1)
		t.b.sem <- struct{}{}
		if t.b.cfg.OnReleased != nil {
			t.b.cfg.OnReleased(t.b.cfg.Name, dur)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// BulkheadGroup: manage multiple named bulkheads as a registry
// ─────────────────────────────────────────────────────────────────────────────

// BulkheadGroup is a thread-safe registry of named bulkheads.
type BulkheadGroup struct {
	mu       sync.RWMutex
	heads    map[string]*Bulkhead
	defaults BulkheadConfig
}

// NewBulkheadGroup creates a group with optional default config for auto-created bulkheads.
func NewBulkheadGroup(defaults BulkheadConfig) *BulkheadGroup {
	return &BulkheadGroup{
		heads:    make(map[string]*Bulkhead),
		defaults: defaults,
	}
}

// Get returns (or creates) the named bulkhead. Use Register for explicit config.
func (g *BulkheadGroup) Get(name string) *Bulkhead {
	g.mu.RLock()
	bh, ok := g.heads[name]
	g.mu.RUnlock()
	if ok {
		return bh
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if bh, ok = g.heads[name]; ok {
		return bh
	}
	cfg := g.defaults
	cfg.Name = name
	bh = NewBulkhead(cfg)
	g.heads[name] = bh
	return bh
}

// Register explicitly registers a bulkhead with the given config.
func (g *BulkheadGroup) Register(cfg BulkheadConfig) *Bulkhead {
	g.mu.Lock()
	defer g.mu.Unlock()
	bh := NewBulkhead(cfg)
	g.heads[cfg.Name] = bh
	return bh
}

// AllStats returns stats for all registered bulkheads.
func (g *BulkheadGroup) AllStats() map[string]BulkheadStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]BulkheadStats, len(g.heads))
	for k, v := range g.heads {
		out[k] = v.Stats()
	}
	return out
}
