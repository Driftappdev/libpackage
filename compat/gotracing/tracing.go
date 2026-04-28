// Package gotracing provides production-grade distributed tracing:
// OpenTelemetry-compatible span model, W3C TraceContext propagation,
// sampling, exporters (stdout, OTLP-HTTP), and context helpers.
package gotracing

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ---- IDs --------------------------------------------------------------------

// TraceID is a 128-bit trace identifier.
type TraceID [16]byte

// SpanID is a 64-bit span identifier.
type SpanID [8]byte

// IsZero reports whether the ID is unset.
func (t TraceID) IsZero() bool { return t == (TraceID{}) }
func (s SpanID) IsZero() bool  { return s == (SpanID{}) }

func (t TraceID) String() string { return hex.EncodeToString(t[:]) }
func (s SpanID) String() string  { return hex.EncodeToString(s[:]) }

func newTraceID() TraceID {
	var id TraceID
	rand.Read(id[:])
	return id
}

func newSpanID() SpanID {
	var id SpanID
	rand.Read(id[:])
	return id
}

// ParseTraceID parses a 32-hex-char trace ID.
func ParseTraceID(s string) (TraceID, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return TraceID{}, fmt.Errorf("gotracing: invalid trace ID %q", s)
	}
	var id TraceID
	copy(id[:], b)
	return id, nil
}

// ParseSpanID parses a 16-hex-char span ID.
func ParseSpanID(s string) (SpanID, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 8 {
		return SpanID{}, fmt.Errorf("gotracing: invalid span ID %q", s)
	}
	var id SpanID
	copy(id[:], b)
	return id, nil
}

// ---- SpanContext ------------------------------------------------------------

// SpanContext carries the immutable identity of a span.
type SpanContext struct {
	TraceID    TraceID
	SpanID     SpanID
	TraceFlags byte // bit 0 = sampled
	TraceState string
}

// IsSampled reports whether the sampled flag is set.
func (sc SpanContext) IsSampled() bool { return sc.TraceFlags&0x01 == 0x01 }

// IsValid reports whether the SpanContext has non-zero IDs.
func (sc SpanContext) IsValid() bool { return !sc.TraceID.IsZero() && !sc.SpanID.IsZero() }

// ---- Span status & kind ----------------------------------------------------

type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

func (k SpanKind) String() string {
	switch k {
	case SpanKindServer:
		return "SERVER"
	case SpanKindClient:
		return "CLIENT"
	case SpanKindProducer:
		return "PRODUCER"
	case SpanKindConsumer:
		return "CONSUMER"
	}
	return "INTERNAL"
}

// ---- Attributes -------------------------------------------------------------

// Attribute is a key-value pair attached to a span.
type Attribute struct {
	Key   string
	Value interface{}
}

// Attr creates an Attribute.
func Attr(key string, value interface{}) Attribute {
	return Attribute{Key: key, Value: value}
}

// ---- Events -----------------------------------------------------------------

// Event is a timestamped log entry within a span.
type Event struct {
	Name       string
	Timestamp  time.Time
	Attributes []Attribute
}

// ---- Span -------------------------------------------------------------------

// Span represents a single unit of work.
type Span struct {
	mu         sync.Mutex
	sc         SpanContext
	parentID   SpanID
	name       string
	kind       SpanKind
	startTime  time.Time
	endTime    time.Time
	attributes []Attribute
	events     []Event
	status     StatusCode
	statusMsg  string
	ended      bool
	tracer     *Tracer
}

// SpanContext returns the span's context.
func (s *Span) SpanContext() SpanContext { return s.sc }

// SetAttribute attaches a key-value pair to the span.
func (s *Span) SetAttribute(attrs ...Attribute) {
	s.mu.Lock()
	s.attributes = append(s.attributes, attrs...)
	s.mu.Unlock()
}

// AddEvent adds a timestamped event.
func (s *Span) AddEvent(name string, attrs ...Attribute) {
	s.mu.Lock()
	s.events = append(s.events, Event{Name: name, Timestamp: time.Now(), Attributes: attrs})
	s.mu.Unlock()
}

// SetStatus sets the span's status.
func (s *Span) SetStatus(code StatusCode, msg string) {
	s.mu.Lock()
	s.status = code
	s.statusMsg = msg
	s.mu.Unlock()
}

// RecordError records an error event and sets status to Error.
func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}
	s.AddEvent("exception", Attr("exception.message", err.Error()))
	s.SetStatus(StatusError, err.Error())
}

// End finalises the span and hands it to the exporter.
func (s *Span) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.endTime = time.Now()
	s.mu.Unlock()
	s.tracer.provider.export(s)
}

// IsRecording reports whether the span is actively recording.
func (s *Span) IsRecording() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.ended && s.sc.IsSampled()
}

// Duration returns the span duration (zero until End is called).
func (s *Span) Duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endTime.IsZero() {
		return 0
	}
	return s.endTime.Sub(s.startTime)
}

// ---- Tracer -----------------------------------------------------------------

// Tracer creates spans.
type Tracer struct {
	name     string
	provider *Provider
}

type contextKey struct{}

// Start creates a new span, optionally inheriting context from ctx.
// Returns a child ctx with the span embedded.
func (t *Tracer) Start(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	cfg := &spanConfig{kind: SpanKindInternal}
	for _, o := range opts {
		o(cfg)
	}

	parentSC := SpanContextFromContext(ctx)

	var traceID TraceID
	var parentID SpanID
	if parentSC.IsValid() {
		traceID = parentSC.TraceID
		parentID = parentSC.SpanID
	} else {
		traceID = newTraceID()
	}

	spanID := newSpanID()

	flags := byte(0)
	if t.provider.sampler.ShouldSample(traceID) {
		flags = 0x01
	}

	sc := SpanContext{TraceID: traceID, SpanID: spanID, TraceFlags: flags}
	span := &Span{
		sc:        sc,
		parentID:  parentID,
		name:      name,
		kind:      cfg.kind,
		startTime: time.Now(),
		tracer:    t,
	}
	if len(cfg.attributes) > 0 {
		span.attributes = cfg.attributes
	}

	return context.WithValue(ctx, contextKey{}, sc), span
}

// SpanOption configures a span at creation time.
type SpanOption func(*spanConfig)

type spanConfig struct {
	kind       SpanKind
	attributes []Attribute
}

// WithSpanKind sets the span kind.
func WithSpanKind(k SpanKind) SpanOption {
	return func(c *spanConfig) { c.kind = k }
}

// WithAttributes pre-populates span attributes.
func WithAttributes(attrs ...Attribute) SpanOption {
	return func(c *spanConfig) { c.attributes = append(c.attributes, attrs...) }
}

// SpanContextFromContext extracts a SpanContext from ctx.
func SpanContextFromContext(ctx context.Context) SpanContext {
	sc, _ := ctx.Value(contextKey{}).(SpanContext)
	return sc
}

// ---- Sampler ----------------------------------------------------------------

// Sampler decides whether a trace should be sampled.
type Sampler interface {
	ShouldSample(traceID TraceID) bool
}

// AlwaysSample samples every trace.
type AlwaysSample struct{}

func (AlwaysSample) ShouldSample(_ TraceID) bool { return true }

// NeverSample disables all sampling.
type NeverSample struct{}

func (NeverSample) ShouldSample(_ TraceID) bool { return false }

// RatioSampler samples approximately ratio (0–1) of traces.
type RatioSampler struct{ Ratio float64 }

func (r RatioSampler) ShouldSample(id TraceID) bool {
	// Use first 8 bytes as a uint64 for deterministic sampling.
	v := uint64(id[0])<<56 | uint64(id[1])<<48 | uint64(id[2])<<40 | uint64(id[3])<<32 |
		uint64(id[4])<<24 | uint64(id[5])<<16 | uint64(id[6])<<8 | uint64(id[7])
	return float64(v)/float64(^uint64(0)) < r.Ratio
}

// ---- Exporter ---------------------------------------------------------------

// Exporter receives completed spans.
type Exporter interface {
	Export(span *SpanData)
}

// SpanData is the serialisable representation of a completed span.
type SpanData struct {
	TraceID    string      `json:"trace_id"`
	SpanID     string      `json:"span_id"`
	ParentID   string      `json:"parent_id,omitempty"`
	Name       string      `json:"name"`
	Kind       string      `json:"kind"`
	StartTime  time.Time   `json:"start_time"`
	EndTime    time.Time   `json:"end_time"`
	DurationMs float64     `json:"duration_ms"`
	Attributes []Attribute `json:"attributes,omitempty"`
	Events     []Event     `json:"events,omitempty"`
	Status     string      `json:"status"`
	StatusMsg  string      `json:"status_msg,omitempty"`
}

// StdoutExporter prints completed spans as JSON to stdout.
type StdoutExporter struct {
	W io.Writer
}

func (e *StdoutExporter) Export(sd *SpanData) {
	b, _ := json.Marshal(sd)
	e.W.Write(append(b, '\n'))
}

// OTLPHTTPExporter ships spans to an OTLP/HTTP endpoint.
type OTLPHTTPExporter struct {
	Endpoint string
	Headers  map[string]string
	client   http.Client
}

func (e *OTLPHTTPExporter) Export(sd *SpanData) {
	b, _ := json.Marshal(sd)
	req, err := http.NewRequest("POST", e.Endpoint, bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// NoopExporter discards all spans.
type NoopExporter struct{}

func (NoopExporter) Export(_ *SpanData) {}

// ---- Provider ---------------------------------------------------------------

// ProviderConfig configures a Provider.
type ProviderConfig struct {
	Sampler   Sampler
	Exporters []Exporter
}

// Provider is the root object that creates Tracers.
type Provider struct {
	mu        sync.RWMutex
	tracers   map[string]*Tracer
	sampler   Sampler
	exporters []Exporter
}

// NewProvider creates a Provider.
func NewProvider(cfg ProviderConfig) *Provider {
	s := cfg.Sampler
	if s == nil {
		s = AlwaysSample{}
	}
	return &Provider{
		tracers:   make(map[string]*Tracer),
		sampler:   s,
		exporters: cfg.Exporters,
	}
}

// Tracer returns a named Tracer (created once, cached).
func (p *Provider) Tracer(name string) *Tracer {
	p.mu.RLock()
	t, ok := p.tracers[name]
	p.mu.RUnlock()
	if ok {
		return t
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok = p.tracers[name]; ok {
		return t
	}
	t = &Tracer{name: name, provider: p}
	p.tracers[name] = t
	return t
}

func (p *Provider) export(s *Span) {
	if len(p.exporters) == 0 {
		return
	}
	s.mu.Lock()
	sd := &SpanData{
		TraceID:    s.sc.TraceID.String(),
		SpanID:     s.sc.SpanID.String(),
		Name:       s.name,
		Kind:       s.kind.String(),
		StartTime:  s.startTime,
		EndTime:    s.endTime,
		DurationMs: float64(s.endTime.Sub(s.startTime).Microseconds()) / 1000,
		Attributes: s.attributes,
		Events:     s.events,
		StatusMsg:  s.statusMsg,
	}
	if !s.parentID.IsZero() {
		sd.ParentID = s.parentID.String()
	}
	switch s.status {
	case StatusOK:
		sd.Status = "OK"
	case StatusError:
		sd.Status = "ERROR"
	default:
		sd.Status = "UNSET"
	}
	s.mu.Unlock()
	for _, e := range p.exporters {
		e.Export(sd)
	}
}

// ---- W3C TraceContext propagation -------------------------------------------

const (
	traceparentHeader = "Traceparent"
	tracestateHeader  = "Tracestate"
)

// Inject writes the SpanContext from ctx into HTTP request headers (W3C TraceContext).
func Inject(ctx context.Context, headers http.Header) {
	sc := SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return
	}
	headers.Set(traceparentHeader, fmt.Sprintf("00-%s-%s-%02x", sc.TraceID, sc.SpanID, sc.TraceFlags))
	if sc.TraceState != "" {
		headers.Set(tracestateHeader, sc.TraceState)
	}
}

// Extract reads W3C TraceContext headers and returns an updated ctx.
func Extract(ctx context.Context, headers http.Header) context.Context {
	tp := headers.Get(traceparentHeader)
	if tp == "" {
		return ctx
	}
	parts := splitN(tp, "-", 4)
	if len(parts) != 4 || parts[0] != "00" {
		return ctx
	}
	traceID, err := ParseTraceID(parts[1])
	if err != nil {
		return ctx
	}
	spanID, err := ParseSpanID(parts[2])
	if err != nil {
		return ctx
	}
	var flags byte
	if len(parts[3]) >= 2 {
		b, _ := hex.DecodeString(parts[3])
		if len(b) > 0 {
			flags = b[0]
		}
	}
	sc := SpanContext{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		TraceState: headers.Get(tracestateHeader),
	}
	return context.WithValue(ctx, contextKey{}, sc)
}

// HTTPMiddleware injects trace context propagation into HTTP servers.
// It extracts incoming TraceContext headers and starts a server span.
func HTTPMiddleware(tracer *Tracer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := Extract(r.Context(), r.Header)
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			WithSpanKind(SpanKindServer),
			WithAttributes(
				Attr("http.method", r.Method),
				Attr("http.url", r.URL.String()),
				Attr("http.host", r.Host),
				Attr("http.user_agent", r.UserAgent()),
			),
		)
		defer span.End()

		rw := &responseWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r.WithContext(ctx))

		span.SetAttribute(Attr("http.status_code", rw.status))
		if rw.status >= 500 {
			span.SetStatus(StatusError, http.StatusText(rw.status))
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func splitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := len(s)
		for j := 0; j < len(s); j++ {
			if s[j:j+len(sep)] == sep {
				idx = j
				break
			}
		}
		result = append(result, s[:idx])
		if idx+len(sep) >= len(s) {
			s = ""
			break
		}
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

// ---- Global default provider ------------------------------------------------

var (
	globalMu       sync.RWMutex
	globalProvider *Provider
)

func init() {
	globalProvider = NewProvider(ProviderConfig{Sampler: AlwaysSample{}, Exporters: []Exporter{NoopExporter{}}})
}

// SetGlobalProvider sets the package-level provider.
func SetGlobalProvider(p *Provider) {
	globalMu.Lock()
	globalProvider = p
	globalMu.Unlock()
}

// GlobalTracer returns a Tracer from the global provider.
func GlobalTracer(name string) *Tracer {
	globalMu.RLock()
	p := globalProvider
	globalMu.RUnlock()
	return p.Tracer(name)
}
