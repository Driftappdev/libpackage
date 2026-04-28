package tracing

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// HTTPConfig configures OpenTelemetry HTTP tracing middleware.
type HTTPConfig struct {
	ServiceName          string
	Tracer               oteltrace.Tracer
	Propagator           propagation.TextMapPropagator
	SkipPaths            []string
	SpanNameFormatter    func(r *http.Request) string
	AdditionalAttributes func(r *http.Request) []attribute.KeyValue
}

// HTTPMiddleware instruments net/http handlers with OpenTelemetry spans.
func HTTPMiddleware(cfg HTTPConfig) func(http.Handler) http.Handler {
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer(cfg.ServiceName)
	}
	propagator := cfg.Propagator
	if propagator == nil {
		propagator = otel.GetTextMapPropagator()
	}
	spanNameFn := cfg.SpanNameFormatter
	if spanNameFn == nil {
		spanNameFn = func(r *http.Request) string { return r.Method + " " + r.URL.Path }
	}

	skip := make(map[string]struct{}, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skip[p] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skip[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(
				ctx,
				spanNameFn(r),
				oteltrace.WithSpanKind(oteltrace.SpanKindServer),
				oteltrace.WithAttributes(
					semconv.HTTPMethod(r.Method),
					semconv.HTTPURL(r.URL.String()),
					semconv.HTTPScheme(httpScheme(r)),
					semconv.NetHostName(r.Host),
					attribute.String("http.user_agent", r.UserAgent()),
				),
			)
			defer span.End()

			if cfg.AdditionalAttributes != nil {
				span.SetAttributes(cfg.AdditionalAttributes(r)...)
			}
			propagator.Inject(ctx, propagation.HeaderCarrier(w.Header()))

			rw := &traceResponseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r.WithContext(ctx))

			span.SetAttributes(semconv.HTTPStatusCode(rw.status))
			if rw.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(rw.status))
				return
			}
			span.SetStatus(codes.Ok, "")
		})
	}
}

type traceResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *traceResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func httpScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if s := r.Header.Get("X-Forwarded-Proto"); s != "" {
		return s
	}
	return "http"
}
