// dlq.go — Enterprise Dead Letter Queue.
//
// Features:
//   - Pluggable storage backend (in-memory, Redis, PostgreSQL, S3, …)
//   - TTL-based automatic expiry with background reaper
//   - Poison-pill detection (repeated-failure fingerprinting)
//   - Retention policy: count + age + size caps
//   - Full failure audit trail per message
//   - Alerting hook (PagerDuty, Slack, SNS, …)
//   - Prometheus-compatible metrics
//   - Thread-safe, zero third-party deps
//   - Manual inspect / delete / replay-to-inbox API
package dlq

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Priority uint8

const (
	PriorityCritical Priority = iota
	PriorityHigh
	PriorityNormal
	PriorityLow
)

type Message struct {
	ID          string
	SenderID    string
	Topic       string
	Priority    Priority
	Body        []byte
	Metadata    map[string]string
	Timestamp   time.Time
	Attempts    int
	MaxAttempts int
}

type ReplayTarget interface {
	Submit(msg *Message) error
}

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
// Public errors
// ---------------------------------------------------------------------------

var (
	ErrDLQClosed          = errors.New("dlq: closed")
	ErrDLQFull            = errors.New("dlq: capacity exceeded")
	ErrDLQMessageNotFound = errors.New("dlq: message not found")
	ErrDLQInvalidFilter   = errors.New("dlq: invalid filter")
)

// ---------------------------------------------------------------------------
// Failure record
// ---------------------------------------------------------------------------

// FailureRecord captures one delivery attempt failure.
type FailureRecord struct {
	AttemptedAt time.Time
	Error       string
	Attempt     int
}

// DeadMessage wraps a Message with its full failure audit trail.
type DeadMessage struct {
	Message    *Message
	Failures   []FailureRecord
	ArrivedAt  time.Time // when it landed in the DLQ
	ExpiresAt  time.Time // zero = never expires
	PoisonPill bool      // flagged by the poison-pill detector
	Replayed   bool      // has been replayed at least once
	Tags       map[string]string
}

func (dm *DeadMessage) LastError() string {
	if len(dm.Failures) == 0 {
		return ""
	}
	return dm.Failures[len(dm.Failures)-1].Error
}

// ---------------------------------------------------------------------------
// DLQ Storage backend
// ---------------------------------------------------------------------------

// DLQStorage abstracts where dead messages are persisted.
// Implement for Redis, PostgreSQL, S3 Glacier, DynamoDB, etc.
type DLQStorage interface {
	// Save persists or upserts a dead message.
	Save(ctx context.Context, dm *DeadMessage) error

	// Get retrieves a dead message by message ID.
	Get(ctx context.Context, id string) (*DeadMessage, error)

	// Delete permanently removes a dead message.
	Delete(ctx context.Context, id string) error

	// List returns dead messages matching the filter, ordered by ArrivedAt desc.
	List(ctx context.Context, f DLQFilter) ([]*DeadMessage, error)

	// Count returns the total number of stored dead messages.
	Count(ctx context.Context) (int64, error)

	// Purge removes all messages matching the filter (e.g., expired).
	Purge(ctx context.Context, f DLQFilter) (int, error)
}

// DLQFilter is a query predicate for listing/purging dead messages.
type DLQFilter struct {
	Topic       string    // "" = all topics
	PoisonOnly  bool      // only poison-pill messages
	Before      time.Time // ArrivedAt < Before (zero = no limit)
	After       time.Time // ArrivedAt > After  (zero = no limit)
	ExpiredOnly bool      // ExpiresAt < now
	Limit       int       // 0 = no limit
	Offset      int
}

// ---------------------------------------------------------------------------
// In-memory DLQ Storage (reference implementation)
// ---------------------------------------------------------------------------

type memDLQEntry struct {
	dm      *DeadMessage
	arrived time.Time
}

type InMemoryDLQStorage struct {
	mu      sync.RWMutex
	entries map[string]*memDLQEntry
	order   []string // insertion order for List
}

func NewInMemoryDLQStorage() *InMemoryDLQStorage {
	return &InMemoryDLQStorage{
		entries: make(map[string]*memDLQEntry, 256),
	}
}

func (s *InMemoryDLQStorage) Save(_ context.Context, dm *DeadMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.entries[dm.Message.ID]; !exists {
		s.order = append(s.order, dm.Message.ID)
	}
	s.entries[dm.Message.ID] = &memDLQEntry{dm: dm, arrived: dm.ArrivedAt}
	return nil
}

func (s *InMemoryDLQStorage) Get(_ context.Context, id string) (*DeadMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, ErrDLQMessageNotFound
	}
	return e.dm, nil
}

func (s *InMemoryDLQStorage) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return ErrDLQMessageNotFound
	}
	delete(s.entries, id)
	// Rebuild order slice (rare operation)
	newOrder := s.order[:0]
	for _, k := range s.order {
		if k != id {
			newOrder = append(newOrder, k)
		}
	}
	s.order = newOrder
	return nil
}

func (s *InMemoryDLQStorage) List(_ context.Context, f DLQFilter) ([]*DeadMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var results []*DeadMessage

	// Collect in reverse insertion order (newest first)
	for i := len(s.order) - 1; i >= 0; i-- {
		id := s.order[i]
		e, ok := s.entries[id]
		if !ok {
			continue
		}
		dm := e.dm
		if f.Topic != "" && dm.Message.Topic != f.Topic {
			continue
		}
		if f.PoisonOnly && !dm.PoisonPill {
			continue
		}
		if !f.Before.IsZero() && !e.arrived.Before(f.Before) {
			continue
		}
		if !f.After.IsZero() && !e.arrived.After(f.After) {
			continue
		}
		if f.ExpiredOnly && (dm.ExpiresAt.IsZero() || dm.ExpiresAt.After(now)) {
			continue
		}
		results = append(results, dm)
	}

	// Sort by ArrivedAt desc (already approximately correct but re-sort for safety)
	sort.Slice(results, func(i, j int) bool {
		return results[i].ArrivedAt.After(results[j].ArrivedAt)
	})

	// Pagination
	if f.Offset > 0 {
		if f.Offset >= len(results) {
			return nil, nil
		}
		results = results[f.Offset:]
	}
	if f.Limit > 0 && len(results) > f.Limit {
		results = results[:f.Limit]
	}
	return results, nil
}

func (s *InMemoryDLQStorage) Count(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.entries)), nil
}

func (s *InMemoryDLQStorage) Purge(_ context.Context, f DLQFilter) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	removed := 0
	for id, e := range s.entries {
		dm := e.dm
		if f.Topic != "" && dm.Message.Topic != f.Topic {
			continue
		}
		if f.ExpiredOnly && (dm.ExpiresAt.IsZero() || dm.ExpiresAt.After(now)) {
			continue
		}
		delete(s.entries, id)
		removed++
	}
	// Rebuild order
	newOrder := make([]string, 0, len(s.entries))
	for _, k := range s.order {
		if _, ok := s.entries[k]; ok {
			newOrder = append(newOrder, k)
		}
	}
	s.order = newOrder
	return removed, nil
}

// ---------------------------------------------------------------------------
// Alerter hook
// ---------------------------------------------------------------------------

// Alerter is called when a threshold event occurs (poison pill, capacity, …).
type Alerter interface {
	Alert(ctx context.Context, event AlertEvent) error
}

type AlertEvent struct {
	Type    string       // "poison_pill" | "capacity_warning" | "expired_purge" | "new_dead"
	Message *DeadMessage // may be nil for aggregate events
	Count   int64        // current DLQ depth
	Details string
}

type noopAlerter struct{}

func (noopAlerter) Alert(_ context.Context, _ AlertEvent) error { return nil }

// ---------------------------------------------------------------------------
// Poison-pill detector
// ---------------------------------------------------------------------------

// poisonDetector tracks failure counts per message fingerprint (topic + sender).
// A message is flagged as a poison pill when it recurs beyond a threshold.
type poisonDetector struct {
	mu        sync.Mutex
	counts    map[string]int
	threshold int
}

func newPoisonDetector(threshold int) *poisonDetector {
	if threshold <= 0 {
		threshold = 5
	}
	return &poisonDetector{
		counts:    make(map[string]int, 64),
		threshold: threshold,
	}
}

func (pd *poisonDetector) fingerprint(msg *Message) string {
	return msg.Topic + "|" + msg.SenderID
}

// Observe records a failure; returns true if the message is a poison pill.
func (pd *poisonDetector) Observe(msg *Message) bool {
	fp := pd.fingerprint(msg)
	pd.mu.Lock()
	pd.counts[fp]++
	count := pd.counts[fp]
	pd.mu.Unlock()
	return count >= pd.threshold
}

func (pd *poisonDetector) Reset(msg *Message) {
	fp := pd.fingerprint(msg)
	pd.mu.Lock()
	delete(pd.counts, fp)
	pd.mu.Unlock()
}

// ---------------------------------------------------------------------------
// DLQ Metrics
// ---------------------------------------------------------------------------

type DLQMetrics interface {
	IncReceived(topic string)
	IncReplayed(topic string)
	IncExpired(topic string)
	IncPoisonPill(topic string)
	IncDeleted(topic string)
	ObserveDepth(depth int64)
}

type noopDLQMetrics struct{}

func (noopDLQMetrics) IncReceived(string)   {}
func (noopDLQMetrics) IncReplayed(string)   {}
func (noopDLQMetrics) IncExpired(string)    {}
func (noopDLQMetrics) IncPoisonPill(string) {}
func (noopDLQMetrics) IncDeleted(string)    {}
func (noopDLQMetrics) ObserveDepth(int64)   {}

// ---------------------------------------------------------------------------
// DLQConfig
// ---------------------------------------------------------------------------

type DLQConfig struct {
	// Storage backend (required)
	Storage DLQStorage

	// Alerter (optional)
	Alerter Alerter

	// Metrics (optional)
	Metrics DLQMetrics

	// Logger (optional)
	Log Logger

	// Default TTL for dead messages (0 = infinite)
	DefaultTTL time.Duration

	// Capacity limit (0 = unlimited)
	MaxMessages int64

	// Poison-pill detection threshold (0 = 5)
	PoisonThreshold int

	// Background reaper interval (0 = 1m)
	ReaperInterval time.Duration

	// Capacity warning at X% full (0 = 80)
	CapacityWarningPct int

	// Replay target (optional; used by Replay API)
	ReplayInbox ReplayTarget

	// Metrics poll interval (0 = 30s)
	MetricsInterval time.Duration
}

func (c *DLQConfig) applyDefaults() {
	if c.Alerter == nil {
		c.Alerter = noopAlerter{}
	}
	if c.Metrics == nil {
		c.Metrics = noopDLQMetrics{}
	}
	if c.Log == nil {
		c.Log = discardLogger{}
	}
	if c.PoisonThreshold == 0 {
		c.PoisonThreshold = 5
	}
	if c.ReaperInterval == 0 {
		c.ReaperInterval = time.Minute
	}
	if c.CapacityWarningPct == 0 {
		c.CapacityWarningPct = 80
	}
	if c.MetricsInterval == 0 {
		c.MetricsInterval = 30 * time.Second
	}
}

// ---------------------------------------------------------------------------
// DLQ
// ---------------------------------------------------------------------------

// DLQ is a fault-tolerant dead letter queue with inspection, replay, and
// lifecycle management capabilities.
type DLQ struct {
	cfg    DLQConfig
	poison *poisonDetector
	closed atomic.Bool
	wg     sync.WaitGroup
	stopCh chan struct{}
	total  atomic.Int64 // approximate, synced from storage periodically
}

// NewDLQ creates and starts a DLQ.
func NewDLQ(cfg DLQConfig) (*DLQ, error) {
	if cfg.Storage == nil {
		return nil, fmt.Errorf("dlq: Storage is required")
	}
	cfg.applyDefaults()

	d := &DLQ{
		cfg:    cfg,
		poison: newPoisonDetector(cfg.PoisonThreshold),
		stopCh: make(chan struct{}),
	}

	d.wg.Add(2)
	go d.reaperLoop()
	go d.metricsLoop()

	return d, nil
}

// Send adds a message to the DLQ along with its last error.
// It implements the DLQSink interface so it can be plugged directly into Outbox.
func (d *DLQ) Send(ctx context.Context, msg *Message, lastErr error) error {
	return d.SendWithHistory(ctx, msg, []FailureRecord{
		{
			AttemptedAt: time.Now(),
			Error:       errStr(lastErr),
			Attempt:     msg.Attempts,
		},
	})
}

// SendWithHistory adds a message with its complete failure history.
func (d *DLQ) SendWithHistory(ctx context.Context, msg *Message, failures []FailureRecord) error {
	if d.closed.Load() {
		return ErrDLQClosed
	}

	// Capacity guard
	if d.cfg.MaxMessages > 0 {
		count, err := d.cfg.Storage.Count(ctx)
		if err != nil {
			d.cfg.Log.Warn("dlq: count check failed", "err", err)
		} else if count >= d.cfg.MaxMessages {
			return ErrDLQFull
		} else {
			// Warn at threshold
			pct := int(count * 100 / d.cfg.MaxMessages)
			if pct >= d.cfg.CapacityWarningPct {
				d.cfg.Log.Warn("dlq: approaching capacity", "pct", pct)
				_ = d.cfg.Alerter.Alert(ctx, AlertEvent{
					Type:    "capacity_warning",
					Count:   count,
					Details: fmt.Sprintf("%d%% full", pct),
				})
			}
		}
	}

	isPoison := d.poison.Observe(msg)

	var expiresAt time.Time
	if d.cfg.DefaultTTL > 0 {
		expiresAt = time.Now().Add(d.cfg.DefaultTTL)
	}

	dm := &DeadMessage{
		Message:    msg,
		Failures:   failures,
		ArrivedAt:  time.Now(),
		ExpiresAt:  expiresAt,
		PoisonPill: isPoison,
	}

	if err := d.cfg.Storage.Save(ctx, dm); err != nil {
		return fmt.Errorf("dlq: save: %w", err)
	}

	d.total.Add(1)
	d.cfg.Metrics.IncReceived(msg.Topic)

	if isPoison {
		d.cfg.Log.Warn("dlq: poison pill detected", "msgID", msg.ID, "topic", msg.Topic)
		d.cfg.Metrics.IncPoisonPill(msg.Topic)
		_ = d.cfg.Alerter.Alert(ctx, AlertEvent{
			Type:    "poison_pill",
			Message: dm,
			Details: fmt.Sprintf("message %s flagged as poison pill", msg.ID),
		})
	}

	_ = d.cfg.Alerter.Alert(ctx, AlertEvent{
		Type:    "new_dead",
		Message: dm,
	})

	return nil
}

// Get retrieves a dead message by ID.
func (d *DLQ) Get(ctx context.Context, id string) (*DeadMessage, error) {
	return d.cfg.Storage.Get(ctx, id)
}

// List returns dead messages matching the filter.
func (d *DLQ) List(ctx context.Context, f DLQFilter) ([]*DeadMessage, error) {
	return d.cfg.Storage.List(ctx, f)
}

// Delete permanently removes a dead message.
func (d *DLQ) Delete(ctx context.Context, id string) error {
	dm, err := d.cfg.Storage.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := d.cfg.Storage.Delete(ctx, id); err != nil {
		return err
	}
	d.poison.Reset(dm.Message)
	d.cfg.Metrics.IncDeleted(dm.Message.Topic)
	return nil
}

// DeleteBatch removes multiple dead messages by ID.
func (d *DLQ) DeleteBatch(ctx context.Context, ids []string) (int, error) {
	removed := 0
	for _, id := range ids {
		if err := d.Delete(ctx, id); err != nil {
			if errors.Is(err, ErrDLQMessageNotFound) {
				continue
			}
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// Replay re-submits dead messages matching the filter to the configured inbox.
// Returns the number of messages successfully re-submitted.
func (d *DLQ) Replay(ctx context.Context, f DLQFilter, target ReplayTarget) (int, error) {
	if target == nil {
		target = d.cfg.ReplayInbox
	}
	if target == nil {
		return 0, fmt.Errorf("dlq: no replay target inbox configured")
	}

	dms, err := d.cfg.Storage.List(ctx, f)
	if err != nil {
		return 0, err
	}

	replayed := 0
	for _, dm := range dms {
		dm.Message.Attempts = 0 // reset counter
		if err := target.Submit(dm.Message); err != nil {
			d.cfg.Log.Error("dlq: replay submit failed", "msgID", dm.Message.ID, "err", err)
			continue
		}
		dm.Replayed = true
		_ = d.cfg.Storage.Save(ctx, dm) // update replay flag
		d.cfg.Metrics.IncReplayed(dm.Message.Topic)
		replayed++
	}
	return replayed, nil
}

// PurgeExpired removes all messages past their TTL. Returns count removed.
func (d *DLQ) PurgeExpired(ctx context.Context) (int, error) {
	n, err := d.cfg.Storage.Purge(ctx, DLQFilter{ExpiredOnly: true})
	if err != nil {
		return 0, err
	}
	d.cfg.Log.Info("dlq: purged expired messages", "count", n)
	return n, nil
}

// Stats returns current DLQ statistics.
func (d *DLQ) Stats(ctx context.Context) (DLQStats, error) {
	count, err := d.cfg.Storage.Count(ctx)
	if err != nil {
		return DLQStats{}, err
	}
	poison, err := d.cfg.Storage.List(ctx, DLQFilter{PoisonOnly: true, Limit: 1})
	poisonCount := int64(0)
	if err == nil && len(poison) > 0 {
		// Full count would require backend support; approximate here
		poisonAll, _ := d.cfg.Storage.List(ctx, DLQFilter{PoisonOnly: true})
		poisonCount = int64(len(poisonAll))
	}
	return DLQStats{
		TotalMessages: count,
		PoisonPills:   poisonCount,
	}, nil
}

type DLQStats struct {
	TotalMessages int64
	PoisonPills   int64
}

// Close shuts down background goroutines.
func (d *DLQ) Close() {
	if d.closed.CompareAndSwap(false, true) {
		close(d.stopCh)
		d.wg.Wait()
	}
}

// ---------------------------------------------------------------------------
// Background goroutines
// ---------------------------------------------------------------------------

func (d *DLQ) reaperLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(d.cfg.ReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			n, err := d.PurgeExpired(ctx)
			cancel()
			if err != nil {
				d.cfg.Log.Error("dlq: reaper error", "err", err)
			} else if n > 0 {
				d.cfg.Metrics.IncExpired("*")
			}
		}
	}
}

func (d *DLQ) metricsLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(d.cfg.MetricsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			count, err := d.cfg.Storage.Count(ctx)
			cancel()
			if err == nil {
				d.cfg.Metrics.ObserveDepth(count)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
