// Package propagation handles cross-process context propagation across
// HTTP headers, gRPC metadata, and event message headers.
// It supports W3C TraceContext, B3 (single & multi-header), and a custom
// enterprise envelope that carries all obslib IDs in a single header.
package propagation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc/metadata"

	"github.com/platformcore/libpackage/middleware/ids"
)

// â”€â”€â”€ Enterprise Envelope Header â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// EnvelopeHeader is a single JSON-encoded header that carries all IDs.
// X-Obs-Context: {"rid":"...","cid":"...","sid":"...","tp":"00-...-...-01"}
const EnvelopeHeader = "X-Obs-Context"
const EnvelopeMeta = "x-obs-context" // gRPC metadata key (lowercase)

type envelope struct {
	RequestID     string `json:"rid,omitempty"`
	CorrelationID string `json:"cid,omitempty"`
	SessionID     string `json:"sid,omitempty"`
	TraceParent   string `json:"tp,omitempty"`
	TraceState    string `json:"ts,omitempty"`
}

// â”€â”€â”€ HTTP Propagator â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// HTTPExtract extracts all obslib IDs from HTTP request headers into ctx.
// It supports: W3C traceparent, B3 headers, and the X-Obs-Context envelope.
func HTTPExtract(ctx context.Context, h http.Header) context.Context {
	carrier := ids.Carrier{}

	// 1. Parse enterprise envelope first (highest precedence).
	if env := h.Get(EnvelopeHeader); env != "" {
		var e envelope
		if err := json.Unmarshal([]byte(env), &e); err == nil {
			carrier.RequestID = ids.RequestID(e.RequestID)
			carrier.CorrelationID = ids.CorrelationID(e.CorrelationID)
			carrier.SessionID = ids.SessionID(e.SessionID)
			if tp, err := ids.ParseTraceParent(e.TraceParent); err == nil {
				carrier.TraceID = tp.TraceID
				carrier.SpanID = tp.SpanID
				carrier.Sampled = tp.Sampled
			}
		}
	}

	// 2. Individual headers (fallback / supplement).
	if carrier.RequestID == "" {
		if v := h.Get(ids.HeaderRequestID); v != "" {
			carrier.RequestID = ids.RequestID(v)
		} else if v := h.Get(ids.HeaderCorrelationID); v != "" {
			// Some gateways send Correlation-ID instead of Request-ID.
			carrier.RequestID = ids.RequestID(v)
		}
	}
	if string(carrier.CorrelationID) == "" {
		carrier.CorrelationID = ids.CorrelationID(h.Get(ids.HeaderCorrelationID))
	}
	if string(carrier.SessionID) == "" {
		carrier.SessionID = ids.SessionID(h.Get(ids.HeaderSessionID))
	}

	// 3. W3C traceparent.
	if carrier.TraceID.IsZero() {
		if tp, err := ids.ParseTraceParent(h.Get(ids.HeaderTraceParent)); err == nil {
			carrier.TraceID = tp.TraceID
			carrier.SpanID = tp.SpanID
			carrier.Sampled = tp.Sampled
		}
	}

	// 4. B3 multi-header.
	if carrier.TraceID.IsZero() {
		if tid, err := ids.ParseTraceID(h.Get(ids.HeaderTraceID)); err == nil {
			carrier.TraceID = tid
		}
		if sid, err := ids.ParseSpanID(h.Get(ids.HeaderSpanID)); err == nil {
			carrier.SpanID = sid
		}
		if psid, err := ids.ParseSpanID(h.Get(ids.HeaderParentSpanID)); err == nil {
			carrier.ParentSpanID = psid
		}
		carrier.Sampled = h.Get(ids.HeaderSampled) == "1"
	}

	// 5. Generate missing IDs.
	if carrier.RequestID == "" {
		carrier.RequestID = ids.NewRequestID()
	}
	if carrier.TraceID.IsZero() {
		carrier.TraceID = ids.NewTraceID()
		carrier.SpanID = ids.NewSpanID()
		carrier.Sampled = true // default: sample new traces
	}

	return ids.WithCarrier(ctx, carrier)
}

// HTTPInject writes all obslib IDs from ctx into HTTP response/request headers.
func HTTPInject(ctx context.Context, h http.Header) {
	carrier := ids.CarrierFromContext(ctx)

	// Enterprise envelope.
	tp := ids.TraceParent{
		TraceID: carrier.TraceID,
		SpanID:  carrier.SpanID,
		Sampled: carrier.Sampled,
	}
	env := envelope{
		RequestID:     string(carrier.RequestID),
		CorrelationID: string(carrier.CorrelationID),
		SessionID:     string(carrier.SessionID),
		TraceParent:   tp.String(),
	}
	if b, err := json.Marshal(env); err == nil {
		h.Set(EnvelopeHeader, string(b))
	}

	// Canonical individual headers.
	if carrier.RequestID != "" {
		h.Set(ids.HeaderRequestID, string(carrier.RequestID))
	}
	if carrier.ResponseID != "" {
		h.Set(ids.HeaderResponseID, string(carrier.ResponseID))
	}
	if carrier.CorrelationID != "" {
		h.Set(ids.HeaderCorrelationID, string(carrier.CorrelationID))
	}

	// W3C traceparent.
	h.Set(ids.HeaderTraceParent, tp.String())

	// B3 multi-header (for interop with older services).
	h.Set(ids.HeaderTraceID, carrier.TraceID.String())
	h.Set(ids.HeaderSpanID, carrier.SpanID.String())
	if !carrier.ParentSpanID.IsZero() {
		h.Set(ids.HeaderParentSpanID, carrier.ParentSpanID.String())
	}
	if carrier.Sampled {
		h.Set(ids.HeaderSampled, "1")
	}
}

// â”€â”€â”€ gRPC Metadata Propagator â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// GRPCExtract extracts obslib IDs from incoming gRPC metadata.
func GRPCExtract(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	carrier := ids.Carrier{}

	// Enterprise envelope.
	if vals := md.Get(EnvelopeMeta); len(vals) > 0 {
		var e envelope
		if err := json.Unmarshal([]byte(vals[0]), &e); err == nil {
			carrier.RequestID = ids.RequestID(e.RequestID)
			carrier.CorrelationID = ids.CorrelationID(e.CorrelationID)
			carrier.SessionID = ids.SessionID(e.SessionID)
			if tp, err := ids.ParseTraceParent(e.TraceParent); err == nil {
				carrier.TraceID = tp.TraceID
				carrier.SpanID = tp.SpanID
				carrier.Sampled = tp.Sampled
			}
		}
	}

	if carrier.RequestID == "" {
		if vals := md.Get(ids.MetaRequestID); len(vals) > 0 {
			carrier.RequestID = ids.RequestID(vals[0])
		}
	}
	if string(carrier.CorrelationID) == "" {
		if vals := md.Get(ids.MetaCorrelationID); len(vals) > 0 {
			carrier.CorrelationID = ids.CorrelationID(vals[0])
		}
	}
	if string(carrier.SessionID) == "" {
		if vals := md.Get(ids.MetaSessionID); len(vals) > 0 {
			carrier.SessionID = ids.SessionID(vals[0])
		}
	}
	if carrier.TraceID.IsZero() {
		if vals := md.Get(ids.MetaTraceParent); len(vals) > 0 {
			if tp, err := ids.ParseTraceParent(vals[0]); err == nil {
				carrier.TraceID = tp.TraceID
				carrier.SpanID = tp.SpanID
				carrier.Sampled = tp.Sampled
			}
		}
	}

	if carrier.RequestID == "" {
		carrier.RequestID = ids.NewRequestID()
	}
	if carrier.TraceID.IsZero() {
		carrier.TraceID = ids.NewTraceID()
		carrier.SpanID = ids.NewSpanID()
		carrier.Sampled = true
	}

	return ids.WithCarrier(ctx, carrier)
}

// GRPCInject writes obslib IDs from ctx into outgoing gRPC metadata.
func GRPCInject(ctx context.Context) context.Context {
	carrier := ids.CarrierFromContext(ctx)
	tp := ids.TraceParent{TraceID: carrier.TraceID, SpanID: carrier.SpanID, Sampled: carrier.Sampled}
	env := envelope{
		RequestID:     string(carrier.RequestID),
		CorrelationID: string(carrier.CorrelationID),
		SessionID:     string(carrier.SessionID),
		TraceParent:   tp.String(),
	}
	md := metadata.Pairs(
		ids.MetaRequestID, string(carrier.RequestID),
		ids.MetaCorrelationID, string(carrier.CorrelationID),
		ids.MetaSessionID, string(carrier.SessionID),
		ids.MetaTraceParent, tp.String(),
	)
	if b, err := json.Marshal(env); err == nil {
		md.Set(EnvelopeMeta, string(b))
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// â”€â”€â”€ Event / Message Propagator â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// MessageHeader is a generic key-value interface for event/message headers.
// Implement this for Kafka, NATS, RabbitMQ, Pub/Sub, etc.
type MessageHeader interface {
	Get(key string) string
	Set(key, value string)
}

// EventExtract extracts obslib IDs from event/message headers.
func EventExtract(ctx context.Context, headers MessageHeader) context.Context {
	carrier := ids.Carrier{}

	if env := headers.Get(EnvelopeHeader); env != "" {
		var e envelope
		if err := json.Unmarshal([]byte(env), &e); err == nil {
			carrier.RequestID = ids.RequestID(e.RequestID)
			carrier.CorrelationID = ids.CorrelationID(e.CorrelationID)
			carrier.SessionID = ids.SessionID(e.SessionID)
			if tp, err := ids.ParseTraceParent(e.TraceParent); err == nil {
				carrier.TraceID = tp.TraceID
				carrier.SpanID = tp.SpanID
				carrier.Sampled = tp.Sampled
			}
		}
	}

	if carrier.RequestID == "" {
		if v := headers.Get(ids.HeaderRequestID); v != "" {
			carrier.RequestID = ids.RequestID(v)
		}
	}
	if carrier.TraceID.IsZero() {
		if v := headers.Get(ids.HeaderTraceParent); v != "" {
			if tp, err := ids.ParseTraceParent(v); err == nil {
				carrier.TraceID = tp.TraceID
				carrier.SpanID = tp.SpanID
				carrier.Sampled = tp.Sampled
			}
		}
	}

	if carrier.RequestID == "" {
		carrier.RequestID = ids.NewRequestID()
	}
	if carrier.TraceID.IsZero() {
		carrier.TraceID = ids.NewTraceID()
		carrier.SpanID = ids.NewSpanID()
		carrier.Sampled = true
	}

	return ids.WithCarrier(ctx, carrier)
}

// EventInject writes obslib IDs from ctx into event/message headers.
func EventInject(ctx context.Context, headers MessageHeader) {
	carrier := ids.CarrierFromContext(ctx)
	tp := ids.TraceParent{TraceID: carrier.TraceID, SpanID: carrier.SpanID, Sampled: carrier.Sampled}
	env := envelope{
		RequestID:     string(carrier.RequestID),
		CorrelationID: string(carrier.CorrelationID),
		SessionID:     string(carrier.SessionID),
		TraceParent:   tp.String(),
	}
	if b, err := json.Marshal(env); err == nil {
		headers.Set(EnvelopeHeader, string(b))
	}
	headers.Set(ids.HeaderRequestID, string(carrier.RequestID))
	headers.Set(ids.HeaderTraceParent, tp.String())
	if carrier.CorrelationID != "" {
		headers.Set(ids.HeaderCorrelationID, string(carrier.CorrelationID))
	}
}

// â”€â”€â”€ Kafka Header Adapter â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// KafkaHeaders adapts a []sarama.RecordHeader (or any []struct{Key, Value []byte}).
// Use this with the EventExtract/EventInject functions.
type KafkaHeaders struct {
	headers map[string]string
}

func NewKafkaHeaders(rawHeaders map[string]string) *KafkaHeaders {
	if rawHeaders == nil {
		rawHeaders = make(map[string]string)
	}
	return &KafkaHeaders{headers: rawHeaders}
}

func (k *KafkaHeaders) Get(key string) string        { return k.headers[strings.ToLower(key)] }
func (k *KafkaHeaders) Set(key, value string)        { k.headers[strings.ToLower(key)] = value }
func (k *KafkaHeaders) All() map[string]string        { return k.headers }
func (k *KafkaHeaders) String() string {
	b, _ := json.Marshal(k.headers)
	return fmt.Sprintf("KafkaHeaders(%s)", b)
}


