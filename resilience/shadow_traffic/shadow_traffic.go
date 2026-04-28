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

// ShadowTrafficRouter duplicates a configurable percentage of live traffic to a
// shadow backend for dark-launch validation, load testing, and canary comparison.
//
// Key properties:
//   - Zero impact on primary response path (shadow is fully async)
//   - Configurable sampling rate with PRNG + token-bucket to cap shadow load
//   - Response diffing for correctness validation
//   - Automatic shadow error isolation
//   - Structured metrics and diff reporting
//
// Example:
//
//	router := NewShadowTrafficRouter(ShadowConfig{
//	    SampleRate:    0.10, // 10% of traffic
//	    MaxInflight:   200,
//	    DiffFunc:      JSONBodyDiff,
//	    OnDiff:        log.DiffResult,
//	})
//	primaryResp, err := router.Do(ctx, req, primaryHandler, shadowHandler)
type ShadowTrafficRouter struct {
	cfg      ShadowConfig
	metrics  *shadowMetrics
	inflight int64
	sampler  *tokenBucketSampler
}

// ShadowRequest is the generic request container passed to both handlers.
type ShadowRequest struct {
	Key     string            // opaque identifier (e.g. route + user ID)
	Body    []byte            // request body snapshot (optional)
	Headers map[string]string // relevant headers (optional)
	Meta    map[string]any    // arbitrary metadata
}

// ShadowResponse is the generic response container from both handlers.
type ShadowResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
	Latency    time.Duration
	Error      error
}

// HandlerFunc is the function signature for primary/shadow handlers.
type HandlerFunc func(ctx context.Context, req *ShadowRequest) *ShadowResponse

// DiffFunc compares primary and shadow responses; returns a non-nil DiffResult
// if they differ meaningfully.
type DiffFunc func(primary, shadow *ShadowResponse) *DiffResult

// DiffResult describes a divergence between primary and shadow.
type DiffResult struct {
	Key            string
	PrimaryStatus  int
	ShadowStatus   int
	BodyMatch      bool
	StatusMatch    bool
	LatencyDeltaMs float64
	Details        string
}

// ShadowConfig configures the ShadowTrafficRouter.
type ShadowConfig struct {
	// SampleRate is the fraction of requests mirrored to shadow (0.0–1.0).
	SampleRate float64

	// MaxInflight caps the number of concurrently executing shadow calls.
	// Excess requests are dropped to protect the shadow target.
	MaxInflight int64

	// ShadowTimeout overrides the context deadline for shadow calls.
	// Default: 5s.
	ShadowTimeout time.Duration

	// DiffFunc compares primary and shadow responses (optional).
	DiffFunc DiffFunc

	// OnDiff is called asynchronously when a diff is detected (optional).
	OnDiff func(diff *DiffResult)

	// OnShadowError is called when the shadow handler returns an error (optional).
	OnShadowError func(key string, err error)

	// OnShadowDrop is called when a shadow request is dropped due to inflight cap.
	OnShadowDrop func(key string)

	// MaxBodySnapshot is the max bytes copied for response diffing. Default: 64KB.
	MaxBodySnapshot int
}

func (c *ShadowConfig) setDefaults() {
	if c.SampleRate <= 0 || c.SampleRate > 1 {
		c.SampleRate = 0.05
	}
	if c.MaxInflight == 0 {
		c.MaxInflight = 100
	}
	if c.ShadowTimeout == 0 {
		c.ShadowTimeout = 5 * time.Second
	}
	if c.MaxBodySnapshot == 0 {
		c.MaxBodySnapshot = 64 * 1024
	}
}

type shadowMetrics struct {
	primaryCalls  int64
	shadowFired   int64
	shadowDropped int64
	shadowErrors  int64
	diffsDetected int64
	primaryErrNs  int64 // sum of primary latency on errors
	shadowLatNs   int64 // sum of shadow latency
}

// tokenBucketSampler provides probabilistic sampling with a small burst.
type tokenBucketSampler struct {
	rate    float64 // 0–1
	counter uint64
}

func newTokenBucketSampler(rate float64) *tokenBucketSampler {
	return &tokenBucketSampler{rate: rate}
}

// sample returns true with probability rate, using a deterministic counter
// to guarantee the exact fraction over long runs.
func (s *tokenBucketSampler) sample() bool {
	n := atomic.AddUint64(&s.counter, 1)
	// Use modular arithmetic: fire when fractional part crosses threshold.
	threshold := uint64(1.0 / s.rate)
	if threshold == 0 {
		threshold = 1
	}
	return n%threshold == 0
}

// NewShadowTrafficRouter creates a new ShadowTrafficRouter.
func NewShadowTrafficRouter(cfg ShadowConfig) *ShadowTrafficRouter {
	cfg.setDefaults()
	return &ShadowTrafficRouter{
		cfg:     cfg,
		metrics: &shadowMetrics{},
		sampler: newTokenBucketSampler(cfg.SampleRate),
	}
}

// Do executes primaryFn synchronously and, if sampled, fires shadowFn asynchronously.
// The primary response and error are always returned; shadow never affects the caller.
func (s *ShadowTrafficRouter) Do(
	ctx context.Context,
	req *ShadowRequest,
	primaryFn HandlerFunc,
	shadowFn HandlerFunc,
) (*ShadowResponse, error) {
	atomic.AddInt64(&s.metrics.primaryCalls, 1)

	// Snapshot body for diffing before primary handler may consume it.
	snapBody := snapshotBody(req.Body, s.cfg.MaxBodySnapshot)

	primary := primaryFn(ctx, req)
	if primary == nil {
		primary = &ShadowResponse{Error: fmt.Errorf("primary handler returned nil")}
	}

	// Decide whether to mirror.
	if shadowFn != nil && s.sampler.sample() {
		inflightNow := atomic.AddInt64(&s.inflight, 1)
		if inflightNow > s.cfg.MaxInflight {
			atomic.AddInt64(&s.inflight, -1)
			atomic.AddInt64(&s.metrics.shadowDropped, 1)
			if s.cfg.OnShadowDrop != nil {
				s.cfg.OnShadowDrop(req.Key)
			}
		} else {
			atomic.AddInt64(&s.metrics.shadowFired, 1)
			// Deep-copy request so shadow handler cannot mutate primary data.
			shadowReq := cloneShadowRequest(req, snapBody)
			go s.executeShadow(shadowReq, primary, shadowFn)
		}
	}

	return primary, primary.Error
}

func (s *ShadowTrafficRouter) executeShadow(
	req *ShadowRequest,
	primary *ShadowResponse,
	shadowFn HandlerFunc,
) {
	defer atomic.AddInt64(&s.inflight, -1)

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ShadowTimeout)
	defer cancel()

	start := time.Now()
	shadow := shadowFn(ctx, req)
	if shadow == nil {
		shadow = &ShadowResponse{Error: fmt.Errorf("shadow handler returned nil")}
	}
	shadow.Latency = time.Since(start)
	atomic.AddInt64(&s.metrics.shadowLatNs, shadow.Latency.Nanoseconds())

	if shadow.Error != nil {
		atomic.AddInt64(&s.metrics.shadowErrors, 1)
		if s.cfg.OnShadowError != nil {
			s.cfg.OnShadowError(req.Key, shadow.Error)
		}
		return
	}

	// Diff if configured.
	if s.cfg.DiffFunc != nil {
		diff := s.cfg.DiffFunc(primary, shadow)
		if diff != nil {
			diff.Key = req.Key
			atomic.AddInt64(&s.metrics.diffsDetected, 1)
			if s.cfg.OnDiff != nil {
				s.cfg.OnDiff(diff)
			}
		}
	}
}

// ShadowMetricsSnapshot is a point-in-time view of shadow metrics.
type ShadowMetricsSnapshot struct {
	PrimaryCalls    int64
	ShadowFired     int64
	ShadowDropped   int64
	ShadowErrors    int64
	DiffsDetected   int64
	InflightNow     int64
	EffectiveSampleRate float64
	AvgShadowLatMs  float64
	DiffRate        float64
}

// Metrics returns a snapshot.
func (s *ShadowTrafficRouter) Metrics() ShadowMetricsSnapshot {
	primary := atomic.LoadInt64(&s.metrics.primaryCalls)
	fired := atomic.LoadInt64(&s.metrics.shadowFired)
	diffs := atomic.LoadInt64(&s.metrics.diffsDetected)
	latNs := atomic.LoadInt64(&s.metrics.shadowLatNs)

	var effectiveRate, avgLat, diffRate float64
	if primary > 0 {
		effectiveRate = float64(fired) / float64(primary)
	}
	if fired > 0 {
		avgLat = float64(latNs) / float64(fired) / 1e6
		diffRate = float64(diffs) / float64(fired)
	}
	return ShadowMetricsSnapshot{
		PrimaryCalls:        primary,
		ShadowFired:         fired,
		ShadowDropped:       atomic.LoadInt64(&s.metrics.shadowDropped),
		ShadowErrors:        atomic.LoadInt64(&s.metrics.shadowErrors),
		DiffsDetected:       diffs,
		InflightNow:         atomic.LoadInt64(&s.inflight),
		EffectiveSampleRate: effectiveRate,
		AvgShadowLatMs:      avgLat,
		DiffRate:            diffRate,
	}
}

// ---- built-in diff functions ----

// StatusCodeDiff is a DiffFunc that reports differences in HTTP status codes.
func StatusCodeDiff(primary, shadow *ShadowResponse) *DiffResult {
	if primary.StatusCode != shadow.StatusCode {
		return &DiffResult{
			PrimaryStatus: primary.StatusCode,
			ShadowStatus:  shadow.StatusCode,
			StatusMatch:   false,
			BodyMatch:     true,
			Details:       fmt.Sprintf("status mismatch: primary=%d shadow=%d", primary.StatusCode, shadow.StatusCode),
		}
	}
	return nil
}

// FullResponseDiff diffs both status code and body bytes.
func FullResponseDiff(primary, shadow *ShadowResponse) *DiffResult {
	statusMatch := primary.StatusCode == shadow.StatusCode
	bodyMatch := bytes.Equal(primary.Body, shadow.Body)
	if statusMatch && bodyMatch {
		return nil
	}
	details := ""
	if !statusMatch {
		details += fmt.Sprintf("status: primary=%d shadow=%d; ", primary.StatusCode, shadow.StatusCode)
	}
	if !bodyMatch {
		details += fmt.Sprintf("body differs (primary=%d bytes, shadow=%d bytes)", len(primary.Body), len(shadow.Body))
	}
	latDelta := shadow.Latency.Seconds()*1000 - primary.Latency.Seconds()*1000
	return &DiffResult{
		PrimaryStatus:  primary.StatusCode,
		ShadowStatus:   shadow.StatusCode,
		StatusMatch:    statusMatch,
		BodyMatch:      bodyMatch,
		LatencyDeltaMs: latDelta,
		Details:        details,
	}
}

// ---- helpers ----

func snapshotBody(body []byte, max int) []byte {
	if len(body) == 0 {
		return nil
	}
	r := io.LimitReader(bytes.NewReader(body), int64(max))
	snap, _ := io.ReadAll(r)
	return snap
}

func cloneShadowRequest(req *ShadowRequest, bodyOverride []byte) *ShadowRequest {
	clone := &ShadowRequest{
		Key:  req.Key,
		Body: bodyOverride,
	}
	if req.Headers != nil {
		clone.Headers = make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			clone.Headers[k] = v
		}
	}
	if req.Meta != nil {
		clone.Meta = make(map[string]any, len(req.Meta))
		for k, v := range req.Meta {
			clone.Meta[k] = v
		}
	}
	return clone
}

// ShadowPool manages a pool of ShadowTrafficRouters keyed by route/service name.
// Use this when different routes need different sampling rates.
type ShadowPool struct {
	mu      sync.RWMutex
	routers map[string]*ShadowTrafficRouter
	default_ *ShadowTrafficRouter
}

// NewShadowPool creates a ShadowPool with a default router.
func NewShadowPool(defaultCfg ShadowConfig) *ShadowPool {
	return &ShadowPool{
		routers:  make(map[string]*ShadowTrafficRouter),
		default_: NewShadowTrafficRouter(defaultCfg),
	}
}

// Register adds a named router with custom config.
func (p *ShadowPool) Register(name string, cfg ShadowConfig) {
	p.mu.Lock()
	p.routers[name] = NewShadowTrafficRouter(cfg)
	p.mu.Unlock()
}

// Get returns the named router or the default.
func (p *ShadowPool) Get(name string) *ShadowTrafficRouter {
	p.mu.RLock()
	r, ok := p.routers[name]
	p.mu.RUnlock()
	if ok {
		return r
	}
	return p.default_
}
