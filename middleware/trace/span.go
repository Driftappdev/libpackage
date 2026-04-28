// Package trace provides an enterprise span management layer on top of
// OpenTelemetry. It enriches spans with standardised enterprise attributes
// and provides helpers for error recording and baggage propagation.
package trace

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/platformcore/libpackage/middleware/ids"
)

// â”€â”€â”€ Semantic Attribute Keys â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

const (
	AttrRequestID     = attribute.Key("request.id")
	AttrResponseID    = attribute.Key("response.id")
	AttrCorrelationID = attribute.Key("correlation.id")
	AttrSessionID     = attribute.Key("session.id")
	AttrTenantID      = attribute.Key("tenant.id")
	AttrUserID        = attribute.Key("user.id")
	AttrServiceName   = attribute.Key("service.name")
	AttrServiceVer    = attribute.Key("service.version")
	AttrEnv           = attribute.Key("deployment.environment")
	AttrRegion        = attribute.Key("cloud.region")
	AttrHTTPMethod    = attribute.Key("http.method")
	AttrHTTPPath      = attribute.Key("http.route")
	AttrHTTPStatus    = attribute.Key("http.status_code")
	AttrGRPCMethod    = attribute.Key("rpc.method")
	AttrGRPCService   = attribute.Key("rpc.service")
	AttrGRPCStatus    = attribute.Key("rpc.grpc.status_code")
	AttrEventType     = attribute.Key("messaging.operation")
	AttrEventTopic    = attribute.Key("messaging.destination")
	AttrEventID       = attribute.Key("messaging.message_id")
	AttrDBSystem      = attribute.Key("db.system")
	AttrDBStatement   = attribute.Key("db.statement")
	AttrErrorType     = attribute.Key("error.type")
	AttrErrorStack    = attribute.Key("error.stack")
)

// â”€â”€â”€ SpanConfig â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// SpanConfig carries static metadata applied to every span.
type SpanConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Region         string
}

// â”€â”€â”€ Tracer â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// Tracer wraps an OpenTelemetry trace.Tracer and automatically enriches
// every span with enterprise-standard attributes.
type Tracer struct {
	otelTracer trace.Tracer
	cfg        SpanConfig
	baseAttrs  []attribute.KeyValue
}

// NewTracer creates a new enterprise Tracer.
func NewTracer(instrumentationName string, cfg SpanConfig) *Tracer {
	return &Tracer{
		otelTracer: otel.Tracer(instrumentationName),
		cfg:        cfg,
		baseAttrs: []attribute.KeyValue{
			AttrServiceName.String(cfg.ServiceName),
			AttrServiceVer.String(cfg.ServiceVersion),
			AttrEnv.String(cfg.Environment),
			AttrRegion.String(cfg.Region),
		},
	}
}

// Config returns the SpanConfig this Tracer was created with.
func (t *Tracer) Config() SpanConfig { return t.cfg }

// Start starts a new span enriched with all context IDs and service metadata.
func (t *Tracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, *Span) {
	carrier := ids.CarrierFromContext(ctx)

	attrs := make([]attribute.KeyValue, 0, len(t.baseAttrs)+6)
	attrs = append(attrs, t.baseAttrs...)
	if carrier.RequestID != "" {
		attrs = append(attrs, AttrRequestID.String(string(carrier.RequestID)))
	}
	if carrier.CorrelationID != "" {
		attrs = append(attrs, AttrCorrelationID.String(string(carrier.CorrelationID)))
	}
	if carrier.SessionID != "" {
		attrs = append(attrs, AttrSessionID.String(string(carrier.SessionID)))
	}

	allOpts := make([]trace.SpanStartOption, 0, len(opts)+1)
	allOpts = append(allOpts, trace.WithAttributes(attrs...))
	allOpts = append(allOpts, opts...)

	ctx, otelSpan := t.otelTracer.Start(ctx, spanName, allOpts...)
	return ctx, &Span{otel: otelSpan, tracer: t, name: spanName, start: time.Now()}
}

// â”€â”€â”€ Span â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// Span wraps an OpenTelemetry span with enterprise helpers.
type Span struct {
	otel   trace.Span
	tracer *Tracer
	name   string
	start  time.Time
}

// OTel returns the underlying OpenTelemetry span.
func (s *Span) OTel() trace.Span { return s.otel }

// SetAttr sets attributes on the span.
func (s *Span) SetAttr(kvs ...attribute.KeyValue) *Span {
	s.otel.SetAttributes(kvs...)
	return s
}

// SetUser records user attributes.
func (s *Span) SetUser(userID, tenantID string) *Span {
	s.otel.SetAttributes(AttrUserID.String(userID), AttrTenantID.String(tenantID))
	return s
}

// SetHTTP records HTTP span attributes.
func (s *Span) SetHTTP(method, path string, status int) *Span {
	s.otel.SetAttributes(
		AttrHTTPMethod.String(method),
		AttrHTTPPath.String(path),
		AttrHTTPStatus.Int(status),
	)
	if status >= 500 {
		s.otel.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
	}
	return s
}

// SetGRPC records gRPC span attributes.
func (s *Span) SetGRPC(service, method string, code uint32) *Span {
	s.otel.SetAttributes(
		AttrGRPCService.String(service),
		AttrGRPCMethod.String(method),
		AttrGRPCStatus.Int(int(code)),
	)
	return s
}

// SetEvent records messaging span attributes.
func (s *Span) SetEvent(topic, eventID, operation string) *Span {
	s.otel.SetAttributes(
		AttrEventTopic.String(topic),
		AttrEventID.String(eventID),
		AttrEventType.String(operation),
	)
	return s
}

// SetDB records database span attributes.
func (s *Span) SetDB(system, statement string) *Span {
	s.otel.SetAttributes(AttrDBSystem.String(system), AttrDBStatement.String(statement))
	return s
}

// RecordError records an error with stack trace on the span.
func (s *Span) RecordError(err error, opts ...trace.EventOption) *Span {
	if err == nil {
		return s
	}
	s.otel.RecordError(err, opts...)
	s.otel.SetStatus(codes.Error, err.Error())
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	s.otel.SetAttributes(
		AttrErrorType.String(fmt.Sprintf("%T", err)),
		AttrErrorStack.String(string(buf[:n])),
	)
	return s
}

// AddEvent adds a named event with attributes to the span timeline.
func (s *Span) AddEvent(name string, attrs ...attribute.KeyValue) *Span {
	s.otel.AddEvent(name, trace.WithAttributes(attrs...))
	return s
}

// End finishes the span.
func (s *Span) End() {
	s.otel.SetAttributes(attribute.Float64("span.duration_ms", float64(time.Since(s.start).Milliseconds())))
	s.otel.End()
}

// EndWithError records err and ends the span.
func (s *Span) EndWithError(err error) { s.RecordError(err); s.End() }

// â”€â”€â”€ Convenience Functions â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// SpanFromContext returns the active OTel span.
func SpanFromContext(ctx context.Context) trace.Span { return trace.SpanFromContext(ctx) }

// TraceIDFromContext returns the trace ID string from context.
func TraceIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ids.GetTraceID(ctx).String()
}

// SpanIDFromContext returns the span ID string from context.
func SpanIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.SpanID().String()
	}
	return ids.GetSpanID(ctx).String()
}


