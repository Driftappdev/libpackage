package trace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type contextKey string

const traceKey contextKey = "midul.trace.id"

func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey, traceID)
}
func TraceIDFromContext(ctx context.Context) string { v, _ := ctx.Value(traceKey).(string); return v }

type Span interface {
	End()
	SetAttribute(key string, value any)
}

type spanKey struct{}

func WithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

func FromContext(ctx context.Context) Span {
	v, _ := ctx.Value(spanKey{}).(Span)
	return v
}

type Exporter string

const (
	ExporterOTLP   Exporter = "otlp"
	ExporterStdout Exporter = "stdout"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Exporter       Exporter
	OTLPEndpoint   string
	Insecure       bool
	SampleRatio    float64
}

type Provider struct {
	tp *sdktrace.TracerProvider
}

func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.ServiceName == "" {
		return nil, errors.New("trace: service name is required")
	}
	if cfg.SampleRatio <= 0 || cfg.SampleRatio > 1 {
		cfg.SampleRatio = 1
	}
	res, err := sdkresource.Merge(
		sdkresource.Default(),
		sdkresource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment.name", cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	var spanExporter sdktrace.SpanExporter
	switch cfg.Exporter {
	case "", ExporterOTLP:
		clientOpts := []otlptracegrpc.Option{}
		if cfg.OTLPEndpoint != "" {
			clientOpts = append(clientOpts, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint))
		}
		if cfg.Insecure {
			clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
		}
		exp, err := otlptracegrpc.New(ctx, clientOpts...)
		if err != nil {
			return nil, err
		}
		spanExporter = exp
	case ExporterStdout:
		exp, err := NewStdoutExporter()
		if err != nil {
			return nil, err
		}
		spanExporter = exp
	default:
		return nil, errors.New("trace: unsupported exporter")
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spanExporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithExportTimeout(10*time.Second),
		),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SampleRatio)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return &Provider{tp: tp}, nil
}

func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.tp == nil {
		return nil
	}
	return p.tp.Shutdown(ctx)
}

func Tracer(name string) oteltrace.Tracer {
	return otel.Tracer(name)
}

func HTTPMiddleware(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, service)
	}
}

type stdoutExporter struct{}

func NewStdoutExporter() (sdktrace.SpanExporter, error) {
	return stdoutExporter{}, nil
}

func (stdoutExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	enc := json.NewEncoder(os.Stdout)
	for _, s := range spans {
		payload := map[string]any{
			"name":       s.Name(),
			"trace_id":   s.SpanContext().TraceID().String(),
			"span_id":    s.SpanContext().SpanID().String(),
			"start":      s.StartTime(),
			"end":        s.EndTime(),
			"attributes": s.Attributes(),
		}
		if err := enc.Encode(payload); err != nil {
			return err
		}
	}
	return nil
}

func (stdoutExporter) Shutdown(context.Context) error { return nil }
