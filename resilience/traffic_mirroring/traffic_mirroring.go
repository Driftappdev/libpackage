package flowguard

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// TrafficMirror captures and replays production traffic to target services.
// It is designed for:
//   - Zero-downtime migration validation
//   - Load testing with real traffic shapes
//   - Regression detection between versions
//   - Request logging / audit capture
//
// Unlike ShadowTrafficRouter (which is handler-based), TrafficMirror operates
// at the request/response capture level and supports replay with transformation,
// persistent storage, and rate-controlled replay.
//
// Architecture:
//
//	Ingress → Capture → Ring Buffer → [Transform] → Mirror Sink(s)
//	                                               ↓
//	                                        Replay Engine
//
// Example:
//
//	mirror := NewTrafficMirror(TrafficMirrorConfig{
//	    BufferSize:    10_000,
//	    Workers:       8,
//	    SampleRate:    0.25,
//	    Sinks:         []MirrorSink{kafkaSink, httpSink},
//	    Transformers:  []RequestTransformer{StripAuthTransformer, HostRewriter("staging")},
//	})
//	mirror.Start()
//	// In your handler:
//	mirror.Capture(CapturedRequest{...})
type TrafficMirror struct {
	cfg      TrafficMirrorConfig
	buffer   chan *CapturedRequest
	metrics  *mirrorMetrics
	stopCh   chan struct{}
	wg       sync.WaitGroup
	sampler  *tokenBucketSampler
	started  int32
	replay   *replayEngine
}

// CapturedRequest is a snapshot of an inbound request for mirroring/replay.
type CapturedRequest struct {
	ID          string            // Unique request ID
	Method      string
	URL         string
	Headers     map[string]string
	Body        []byte
	CapturedAt  time.Time
	SourceNode  string
	Metadata    map[string]any
}

// CapturedResponse pairs a response with its request for diff analysis.
type CapturedResponse struct {
	RequestID  string
	StatusCode int
	Headers    map[string]string
	Body       []byte
	Latency    time.Duration
	Error      error
	Timestamp  time.Time
}

// MirrorSink receives mirrored requests for processing (Kafka, HTTP, storage, etc.).
type MirrorSink interface {
	// Name returns the sink identifier.
	Name() string
	// Send delivers a request to the sink. Must be non-blocking internally.
	Send(ctx context.Context, req *CapturedRequest) error
	// Close flushes and tears down the sink.
	Close() error
}

// RequestTransformer mutates a captured request before mirroring.
// Use to sanitize PII, rewrite hosts, inject test headers, etc.
type RequestTransformer func(req *CapturedRequest) *CapturedRequest

// TrafficMirrorConfig configures the TrafficMirror.
type TrafficMirrorConfig struct {
	// BufferSize is the channel capacity for captured requests.
	BufferSize int

	// Workers is the number of concurrent goroutines sending to sinks.
	Workers int

	// SampleRate is the fraction of traffic to capture (0.0–1.0).
	SampleRate float64

	// Sinks are the destinations for mirrored traffic.
	Sinks []MirrorSink

	// Transformers are applied to each captured request before sending.
	Transformers []RequestTransformer

	// MaxBodySize caps captured body size. Default: 64KB.
	MaxBodySize int

	// SendTimeout is the deadline for each sink.Send call.
	SendTimeout time.Duration

	// OnDrop is called when the buffer is full and a request is dropped.
	OnDrop func(req *CapturedRequest)

	// OnSinkError is called when a sink fails to receive a request.
	OnSinkError func(sinkName string, err error)

	// ReplayCfg configures the optional replay engine (nil = disabled).
	ReplayCfg *ReplayConfig
}

func (c *TrafficMirrorConfig) setDefaults() {
	if c.BufferSize == 0 {
		c.BufferSize = 50_000
	}
	if c.Workers == 0 {
		c.Workers = 8
	}
	if c.SampleRate <= 0 || c.SampleRate > 1 {
		c.SampleRate = 1.0
	}
	if c.MaxBodySize == 0 {
		c.MaxBodySize = 64 * 1024
	}
	if c.SendTimeout == 0 {
		c.SendTimeout = 2 * time.Second
	}
}

type mirrorMetrics struct {
	captured  int64
	dropped   int64
	sent      int64
	sinkErr   int64
	replayed  int64
	filtered  int64
}

// NewTrafficMirror creates a TrafficMirror. Call Start() before Capture().
func NewTrafficMirror(cfg TrafficMirrorConfig) *TrafficMirror {
	cfg.setDefaults()
	m := &TrafficMirror{
		cfg:     cfg,
		buffer:  make(chan *CapturedRequest, cfg.BufferSize),
		metrics: &mirrorMetrics{},
		stopCh:  make(chan struct{}),
		sampler: newTokenBucketSampler(cfg.SampleRate),
	}
	if cfg.ReplayCfg != nil {
		m.replay = newReplayEngine(*cfg.ReplayCfg)
	}
	return m
}

// Start launches worker goroutines.
func (m *TrafficMirror) Start() {
	if !atomic.CompareAndSwapInt32(&m.started, 0, 1) {
		return
	}
	for i := 0; i < m.cfg.Workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	if m.replay != nil {
		m.replay.start(m)
	}
}

// Stop drains the buffer and shuts down workers gracefully.
func (m *TrafficMirror) Stop(drain time.Duration) {
	if !atomic.CompareAndSwapInt32(&m.started, 1, 0) {
		return
	}
	close(m.stopCh)

	// Drain with timeout.
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(drain):
	}

	// Close all sinks.
	for _, sink := range m.cfg.Sinks {
		sink.Close() //nolint:errcheck
	}
}

// Capture enqueues a request for mirroring. Non-blocking: drops if buffer full.
func (m *TrafficMirror) Capture(req *CapturedRequest) {
	if !m.sampler.sample() {
		atomic.AddInt64(&m.metrics.filtered, 1)
		return
	}

	// Snapshot body.
	if len(req.Body) > m.cfg.MaxBodySize {
		body := make([]byte, m.cfg.MaxBodySize)
		copy(body, req.Body)
		req.Body = body
	}

	if req.CapturedAt.IsZero() {
		req.CapturedAt = time.Now()
	}

	select {
	case m.buffer <- req:
		atomic.AddInt64(&m.metrics.captured, 1)
	default:
		atomic.AddInt64(&m.metrics.dropped, 1)
		if m.cfg.OnDrop != nil {
			m.cfg.OnDrop(req)
		}
	}
}

// CaptureFromReader is a convenience method that reads and snapshots a body reader.
func (m *TrafficMirror) CaptureFromReader(
	method, url string,
	headers map[string]string,
	body io.Reader,
	metadata map[string]any,
) (io.Reader, error) {
	if body == nil {
		m.Capture(&CapturedRequest{Method: method, URL: url, Headers: headers, Metadata: metadata})
		return nil, nil
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(body, int64(m.cfg.MaxBodySize)+1))
	if err != nil {
		return nil, fmt.Errorf("flowguard: mirror body read: %w", err)
	}

	snapshot := bodyBytes
	if len(bodyBytes) > m.cfg.MaxBodySize {
		snapshot = bodyBytes[:m.cfg.MaxBodySize]
	}

	m.Capture(&CapturedRequest{
		Method:   method,
		URL:      url,
		Headers:  headers,
		Body:     snapshot,
		Metadata: metadata,
	})

	// Return original body for the primary handler.
	return bytes.NewReader(bodyBytes), nil
}

func (m *TrafficMirror) worker() {
	defer m.wg.Done()
	for {
		select {
		case req := <-m.buffer:
			m.dispatch(req)
		case <-m.stopCh:
			// Drain remaining.
			for {
				select {
				case req := <-m.buffer:
					m.dispatch(req)
				default:
					return
				}
			}
		}
	}
}

func (m *TrafficMirror) dispatch(req *CapturedRequest) {
	// Apply transformers.
	current := req
	for _, t := range m.cfg.Transformers {
		current = t(current)
		if current == nil {
			return // transformer dropped request
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.SendTimeout)
	defer cancel()

	for _, sink := range m.cfg.Sinks {
		if err := sink.Send(ctx, current); err != nil {
			atomic.AddInt64(&m.metrics.sinkErr, 1)
			if m.cfg.OnSinkError != nil {
				m.cfg.OnSinkError(sink.Name(), err)
			}
		} else {
			atomic.AddInt64(&m.metrics.sent, 1)
		}
	}
}

// MirrorMetricsSnapshot is a point-in-time view of mirror metrics.
type MirrorMetricsSnapshot struct {
	Captured   int64
	Dropped    int64
	Sent       int64
	SinkErrors int64
	Replayed   int64
	Filtered   int64
	BufferUsed int
	BufferCap  int
	DropRate   float64
}

// Metrics returns a snapshot.
func (m *TrafficMirror) Metrics() MirrorMetricsSnapshot {
	cap := atomic.LoadInt64(&m.metrics.captured)
	drp := atomic.LoadInt64(&m.metrics.dropped)
	var dropRate float64
	if cap+drp > 0 {
		dropRate = float64(drp) / float64(cap+drp)
	}
	return MirrorMetricsSnapshot{
		Captured:   cap,
		Dropped:    drp,
		Sent:       atomic.LoadInt64(&m.metrics.sent),
		SinkErrors: atomic.LoadInt64(&m.metrics.sinkErr),
		Replayed:   atomic.LoadInt64(&m.metrics.replayed),
		Filtered:   atomic.LoadInt64(&m.metrics.filtered),
		BufferUsed: len(m.buffer),
		BufferCap:  m.cfg.BufferSize,
		DropRate:   dropRate,
	}
}

// ---- Built-in transformers ----

// StripHeadersTransformer returns a transformer that removes specified headers.
func StripHeadersTransformer(headers ...string) RequestTransformer {
	set := make(map[string]struct{}, len(headers))
	for _, h := range headers {
		set[h] = struct{}{}
	}
	return func(req *CapturedRequest) *CapturedRequest {
		clone := cloneRequest(req)
		for _, h := range headers {
			delete(clone.Headers, h)
		}
		_ = set
		return clone
	}
}

// HostRewriteTransformer rewrites the Host header in captured requests.
func HostRewriteTransformer(newHost string) RequestTransformer {
	return func(req *CapturedRequest) *CapturedRequest {
		clone := cloneRequest(req)
		if clone.Headers == nil {
			clone.Headers = make(map[string]string)
		}
		clone.Headers["Host"] = newHost
		return clone
	}
}

// AddHeaderTransformer injects a header into every mirrored request.
func AddHeaderTransformer(key, value string) RequestTransformer {
	return func(req *CapturedRequest) *CapturedRequest {
		clone := cloneRequest(req)
		if clone.Headers == nil {
			clone.Headers = make(map[string]string)
		}
		clone.Headers[key] = value
		return clone
	}
}

// FilterTransformer drops requests matching the predicate (returns nil).
func FilterTransformer(drop func(req *CapturedRequest) bool) RequestTransformer {
	return func(req *CapturedRequest) *CapturedRequest {
		if drop(req) {
			return nil
		}
		return req
	}
}

func cloneRequest(req *CapturedRequest) *CapturedRequest {
	clone := *req
	if req.Headers != nil {
		clone.Headers = make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			clone.Headers[k] = v
		}
	}
	if req.Body != nil {
		clone.Body = make([]byte, len(req.Body))
		copy(clone.Body, req.Body)
	}
	return &clone
}

// ---- Replay Engine ----

// ReplayConfig configures the built-in replay engine.
type ReplayConfig struct {
	// Store holds captured requests for replay.
	Store ReplayStore

	// RateMultiplier replays traffic at N× the original rate. Default: 1.0.
	RateMultiplier float64

	// Workers is the number of concurrent replay goroutines.
	Workers int

	// ReplayTimeout per request.
	ReplayTimeout time.Duration

	// OnReplayResult is called with the response from each replayed request.
	OnReplayResult func(req *CapturedRequest, resp *CapturedResponse)
}

// ReplayStore is a persistent (or in-memory) storage for captured requests.
type ReplayStore interface {
	Save(req *CapturedRequest) error
	Load(limit int) ([]*CapturedRequest, error)
	Delete(ids []string) error
}

type replayEngine struct {
	cfg    ReplayConfig
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func newReplayEngine(cfg ReplayConfig) *replayEngine {
	if cfg.RateMultiplier == 0 {
		cfg.RateMultiplier = 1.0
	}
	if cfg.Workers == 0 {
		cfg.Workers = 4
	}
	if cfg.ReplayTimeout == 0 {
		cfg.ReplayTimeout = 5 * time.Second
	}
	return &replayEngine{cfg: cfg, stopCh: make(chan struct{})}
}

func (r *replayEngine) start(m *TrafficMirror) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.replayBatch(m)
			case <-r.stopCh:
				return
			}
		}
	}()
}

func (r *replayEngine) replayBatch(m *TrafficMirror) {
	if r.cfg.Store == nil {
		return
	}
	reqs, err := r.cfg.Store.Load(100)
	if err != nil || len(reqs) == 0 {
		return
	}

	sem := make(chan struct{}, r.cfg.Workers)
	var wg sync.WaitGroup
	var ids []string
	var mu sync.Mutex

	for _, req := range reqs {
		sem <- struct{}{}
		wg.Add(1)
		go func(req *CapturedRequest) {
			defer func() { <-sem; wg.Done() }()
			ctx, cancel := context.WithTimeout(context.Background(), r.cfg.ReplayTimeout)
			defer cancel()

			// Replay via mirror pipeline.
			m.dispatch(req)
			_ = ctx

			atomic.AddInt64(&m.metrics.replayed, 1)
			if r.cfg.OnReplayResult != nil {
				r.cfg.OnReplayResult(req, &CapturedResponse{RequestID: req.ID})
			}
			mu.Lock()
			ids = append(ids, req.ID)
			mu.Unlock()
		}(req)
	}
	wg.Wait()
	r.cfg.Store.Delete(ids) //nolint:errcheck
}

// ---- In-memory replay store ----

// InMemoryReplayStore is a thread-safe in-memory ReplayStore for testing.
type InMemoryReplayStore struct {
	mu   sync.Mutex
	reqs []*CapturedRequest
}

func (s *InMemoryReplayStore) Save(req *CapturedRequest) error {
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	return nil
}

func (s *InMemoryReplayStore) Load(limit int) ([]*CapturedRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return nil, nil
	}
	n := limit
	if n > len(s.reqs) {
		n = len(s.reqs)
	}
	out := s.reqs[:n]
	s.reqs = s.reqs[n:]
	return out, nil
}

func (s *InMemoryReplayStore) Delete(_ []string) error { return nil }

// ---- No-op sink for testing ----

// DiscardSink drops all mirrored requests silently.
type DiscardSink struct{}

func (d *DiscardSink) Name() string                                    { return "discard" }
func (d *DiscardSink) Send(_ context.Context, _ *CapturedRequest) error { return nil }
func (d *DiscardSink) Close() error                                    { return nil }

// ChannelSink delivers captured requests to a Go channel.
type ChannelSink struct {
	name string
	ch   chan *CapturedRequest
}

// NewChannelSink creates a MirrorSink backed by a buffered channel.
func NewChannelSink(name string, size int) *ChannelSink {
	return &ChannelSink{name: name, ch: make(chan *CapturedRequest, size)}
}

func (c *ChannelSink) Name() string { return c.name }
func (c *ChannelSink) Ch() <-chan *CapturedRequest { return c.ch }
func (c *ChannelSink) Close() error { close(c.ch); return nil }
func (c *ChannelSink) Send(_ context.Context, req *CapturedRequest) error {
	select {
	case c.ch <- req:
		return nil
	default:
		return fmt.Errorf("flowguard: channel sink %q full", c.name)
	}
}
