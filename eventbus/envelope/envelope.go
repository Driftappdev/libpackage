package eventbus

import "time"

type Envelope struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Source     string            `json:"source,omitempty"`
	OccurredAt time.Time         `json:"occurred_at"`
	Headers    map[string]string `json:"headers,omitempty"`
	Payload    []byte            `json:"payload"`
}
