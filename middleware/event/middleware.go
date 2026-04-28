// Package event provides enterprise middleware for event-driven systems
// (Kafka, NATS, RabbitMQ, Google Pub/Sub, AWS SQS, etc.).
// It handles ID extraction from message headers, trace context propagation,
// structured logging, metrics, and retry/dead-letter-queue instrumentation.
package event

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"github.com/platformcore/libpackage/middleware/ids"
	"github.com/platformcore/libpackage/middleware/propagation"
	obttrace "github.com/platformcore/libpackage/middleware/trace"
)

// â”€â”€â”€ Message â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// Message is a generic interface that any message broker message must satisfy.
// Adapt your broker's message type to this interface.
type Message interface {
	// Topic returns the topic/subject/queue name.
	Topic() string
	// ID returns the broker-assigned message ID.
	ID() string
	// Headers returns the message headers (used for propagation).
	Headers() propagation.MessageHeader
	// Body returns the raw message payload.
	Body() []byte
	// Metadata returns broker-specific metadata (partition, offset, etc.).
	Metadata() map[string]string
}

// â”€â”€â”€ Handler â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// Handler is the core event processing function.
type Handler func(ctx context.Context, msg Message) error

// Middleware wraps a Handler.
type Middleware func(Handler) Handler

// Chain composes event middleware (first = outermost).
func Chain(mws ...Middleware) Middleware {
	return func(final Handler) Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			final = mws[i](final)
		}
		return final
	}
}

// â”€â”€â”€ EventConfig â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// EventConfig configures the event observability middleware stack.
type EventConfig struct {
	Tracer      *obttrace.Tracer
	Logger      *zap.Logger
	Metrics     *EventMetrics
	ServiceName string
	// TopicsToSkip bypasses observability for specific topics (e.g. heartbeats).
	TopicsToSkip []string
}

// EventMetrics holds Prometheus metrics for event consumers.
type EventMetrics struct {
	MessagesProcessed *prometheus.CounterVec
	ProcessingLatency *prometheus.HistogramVec
	MessagesInFlight  prometheus.Gauge
	RetryCount        *prometheus.CounterVec
	DLQCount          *prometheus.CounterVec
	MessageAgeSeconds *prometheus.HistogramVec
}

// DefaultEventMetrics registers standard event processing metrics.
func DefaultEventMetrics(reg prometheus.Registerer, namespace string) *EventMetrics {
	if namespace == "" {
		namespace = "obs"
	}
	m := &EventMetrics{
		MessagesProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "event", Name: "messages_processed_total",
			Help: "Total event messages processed.",
		}, []string{"topic", "status", "service"}),
		ProcessingLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Subsystem: "event", Name: "processing_duration_seconds",
			Help:    "Event processing latency.",
			Buckets: []float64{.001, .005, .01, .05, .1, .5, 1, 5, 10, 30},
		}, []string{"topic", "status", "service"}),
		MessagesInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Subsystem: "event", Name: "messages_in_flight",
			Help: "Events currently being processed.",
		}),
		RetryCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "event", Name: "retries_total",
			Help: "Total event retry attempts.",
		}, []string{"topic", "service"}),
		DLQCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "event", Name: "dlq_total",
			Help: "Total messages sent to dead-letter queue.",
		}, []string{"topic", "service"}),
		MessageAgeSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Subsystem: "event", Name: "message_age_seconds",
			Help:    "Age of messages at processing time (publish-to-consume latency).",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 600},
		}, []string{"topic", "service"}),
	}
	if reg != nil {
		reg.MustRegister(
			m.MessagesProcessed, m.ProcessingLatency, m.MessagesInFlight,
			m.RetryCount, m.DLQCount, m.MessageAgeSeconds,
		)
	}
	return m
}

// â”€â”€â”€ WithPropagation â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// WithPropagation extracts obslib IDs from message headers and injects them
// into the processing context. MUST be the outermost middleware.
func WithPropagation() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, msg Message) error {
			ctx = propagation.EventExtract(ctx, msg.Headers())
			// Override span ID with a new child span ID for this consumption.
			carrier := ids.CarrierFromContext(ctx)
			carrier.ParentSpanID = carrier.SpanID
			carrier.SpanID = ids.NewSpanID()
			return next(ids.WithCarrier(ctx, carrier), msg)
		}
	}
}

// â”€â”€â”€ WithObservability â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// WithObservability provides tracing + logging + metrics for each message.
func WithObservability(cfg EventConfig) Middleware {
	skipSet := make(map[string]struct{}, len(cfg.TopicsToSkip))
	for _, t := range cfg.TopicsToSkip {
		skipSet[t] = struct{}{}
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, msg Message) error {
			if _, skip := skipSet[msg.Topic()]; skip {
				return next(ctx, msg)
			}

			carrier := ids.CarrierFromContext(ctx)
			start := time.Now()

			// â”€â”€ Tracing â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
			spanName := fmt.Sprintf("event.consume %s", msg.Topic())
			ctx, span := cfg.Tracer.Start(ctx, spanName)
			span.SetEvent(msg.Topic(), msg.ID(), "receive")
			span.SetAttr(
				attribute.String("request.id", string(carrier.RequestID)),
				attribute.String("message.id", msg.ID()),
				attribute.String("message.topic", msg.Topic()),
				attribute.Int("message.body_size", len(msg.Body())),
			)
			for k, v := range msg.Metadata() {
				span.SetAttr(attribute.String("message.meta."+k, v))
			}

			// â”€â”€ Metrics: in-flight â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
			if cfg.Metrics != nil {
				cfg.Metrics.MessagesInFlight.Inc()
				defer cfg.Metrics.MessagesInFlight.Dec()
			}

			// â”€â”€ Process â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
			err := next(ctx, msg)
			latency := time.Since(start)

			statusLabel := "success"
			if err != nil {
				statusLabel = "error"
				span.RecordError(err)
			}
			span.End()

			// â”€â”€ Metrics â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
			svcName := cfg.ServiceName
			if cfg.Metrics != nil {
				lbls := []string{msg.Topic(), statusLabel, svcName}
				cfg.Metrics.MessagesProcessed.WithLabelValues(lbls...).Inc()
				cfg.Metrics.ProcessingLatency.WithLabelValues(lbls...).Observe(latency.Seconds())
			}

			// â”€â”€ Logging â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
			fields := []zap.Field{
				zap.String("request_id", string(carrier.RequestID)),
				zap.String("correlation_id", string(carrier.CorrelationID)),
				zap.String("trace_id", obttrace.TraceIDFromContext(ctx)),
				zap.String("span_id", obttrace.SpanIDFromContext(ctx)),
				zap.String("topic", msg.Topic()),
				zap.String("message_id", msg.ID()),
				zap.Int("body_size", len(msg.Body())),
				zap.Duration("latency", latency),
				zap.String("status", statusLabel),
				zap.String("service", svcName),
			}
			if err != nil {
				cfg.Logger.Error("event processing failed", append(fields, zap.Error(err))...)
			} else {
				cfg.Logger.Info("event processed", fields...)
			}

			return err
		}
	}
}

// â”€â”€â”€ WithRetry â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// RetryConfig configures retry behaviour for event handlers.
type RetryConfig struct {
	MaxAttempts int
	Backoff     func(attempt int) time.Duration
	// ShouldRetry determines if the error warrants a retry. Default: always retry.
	ShouldRetry func(err error) bool
	// OnRetry is called before each retry.
	OnRetry func(ctx context.Context, msg Message, attempt int, err error)
	// OnDLQ is called when all retries are exhausted.
	OnDLQ func(ctx context.Context, msg Message, err error)
	// Metrics for retry/DLQ tracking.
	Metrics     *EventMetrics
	ServiceName string
}

// ExponentialBackoff returns a standard exponential backoff function.
func ExponentialBackoff(base time.Duration) func(int) time.Duration {
	return func(attempt int) time.Duration {
		d := base
		for i := 0; i < attempt; i++ {
			d *= 2
		}
		if d > 30*time.Second {
			d = 30 * time.Second
		}
		return d
	}
}

// WithRetry wraps a Handler with retry logic and DLQ instrumentation.
func WithRetry(cfg RetryConfig) Middleware {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.Backoff == nil {
		cfg.Backoff = ExponentialBackoff(100 * time.Millisecond)
	}
	if cfg.ShouldRetry == nil {
		cfg.ShouldRetry = func(_ error) bool { return true }
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, msg Message) error {
			var lastErr error
			for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
				if attempt > 0 {
					backoff := cfg.Backoff(attempt - 1)
					if cfg.OnRetry != nil {
						cfg.OnRetry(ctx, msg, attempt, lastErr)
					}
					if cfg.Metrics != nil {
						cfg.Metrics.RetryCount.WithLabelValues(msg.Topic(), cfg.ServiceName).Inc()
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(backoff):
					}
				}
				lastErr = next(ctx, msg)
				if lastErr == nil {
					return nil
				}
				if !cfg.ShouldRetry(lastErr) {
					break
				}
			}
			// All retries exhausted.
			if cfg.Metrics != nil {
				cfg.Metrics.DLQCount.WithLabelValues(msg.Topic(), cfg.ServiceName).Inc()
			}
			if cfg.OnDLQ != nil {
				cfg.OnDLQ(ctx, msg, lastErr)
			}
			return fmt.Errorf("event: all %d retries exhausted for topic %s: %w",
				cfg.MaxAttempts, msg.Topic(), lastErr)
		}
	}
}

// â”€â”€â”€ WithRecovery â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// WithRecovery recovers from handler panics and returns them as errors
// so the retry middleware can handle them.
func WithRecovery(log *zap.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, msg Message) (retErr error) {
			defer func() {
				if rec := recover(); rec != nil {
					carrier := ids.CarrierFromContext(ctx)
					log.Error("event handler panic",
						zap.Any("panic", rec),
						zap.String("topic", msg.Topic()),
						zap.String("message_id", msg.ID()),
						zap.String("request_id", string(carrier.RequestID)),
					)
					retErr = fmt.Errorf("panic in event handler: %v", rec)
				}
			}()
			return next(ctx, msg)
		}
	}
}

// â”€â”€â”€ Publisher â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// PublishFunc is the function signature for publishing a message.
type PublishFunc func(ctx context.Context, topic string, headers propagation.MessageHeader, body []byte) error

// InstrumentedPublisher wraps a PublishFunc with obslib ID injection and tracing.
func InstrumentedPublisher(tracer *obttrace.Tracer, log *zap.Logger, publish PublishFunc) PublishFunc {
	return func(ctx context.Context, topic string, headers propagation.MessageHeader, body []byte) error {
		// Inject current context IDs into outgoing message headers.
		propagation.EventInject(ctx, headers)

		// Start a producer span.
		spanName := fmt.Sprintf("event.publish %s", topic)
		_, span := tracer.Start(ctx, spanName)
		carrier := ids.CarrierFromContext(ctx)
		span.SetEvent(topic, "", "send")
		span.SetAttr(
			attribute.String("request.id", string(carrier.RequestID)),
			attribute.String("message.topic", topic),
			attribute.Int("message.body_size", len(body)),
		)

		err := publish(ctx, topic, headers, body)
		if err != nil {
			span.RecordError(err)
			log.Error("event publish failed",
				zap.String("topic", topic),
				zap.String("request_id", string(carrier.RequestID)),
				zap.Error(err),
			)
		} else {
			log.Debug("event published",
				zap.String("topic", topic),
				zap.String("request_id", string(carrier.RequestID)),
				zap.Int("body_size", len(body)),
			)
		}
		span.End()
		return err
	}
}


