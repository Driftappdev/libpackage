package envelope

import "time"

type Envelope struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Source      string            `json:"source"`
	Subject     string            `json:"subject,omitempty"`
	OccurredAt  time.Time         `json:"occurred_at"`
	TraceID     string            `json:"trace_id,omitempty"`
	Correlation string            `json:"correlation_id,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Payload     []byte            `json:"payload"`
}
