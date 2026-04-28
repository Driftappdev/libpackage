package types

import "time"

// Metadata is a key-value bag that can be attached to domain results.
type Metadata map[string]string

// Status is a normalized service/resource status value.
type Status string

const (
	StatusUnknown  Status = "unknown"
	StatusUp       Status = "up"
	StatusDown     Status = "down"
	StatusDegraded Status = "degraded"
)

// Result is a common status payload shape used across health/reporting APIs.
type Result struct {
	Name      string            `json:"name" yaml:"name"`
	Status    Status            `json:"status" yaml:"status"`
	Message   string            `json:"message,omitempty" yaml:"message,omitempty"`
	Duration  time.Duration     `json:"duration,omitempty" yaml:"duration,omitempty"`
	Error     string            `json:"error,omitempty" yaml:"error,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	CheckedAt time.Time         `json:"checked_at" yaml:"checked_at"`
}

// Named marks values that expose a stable name.
type Named interface {
	Name() string
}
