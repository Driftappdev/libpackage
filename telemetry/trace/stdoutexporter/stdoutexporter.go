package trace

import (
	"context"
	"encoding/json"
	"os"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

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
