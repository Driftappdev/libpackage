package gospan

import (
	"context"
	"time"
)

type ctxKeyType string

const ctxKey ctxKeyType = "span"

type Span struct {
	Name      string    `json:"name"`
	TraceID   string    `json:"trace_id"`
	SpanID    string    `json:"span_id"`
	ParentID  string    `json:"parent_id,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

func Start(name, traceID, spanID, parentID string) *Span {
	return &Span{
		Name:      name,
		TraceID:   traceID,
		SpanID:    spanID,
		ParentID:  parentID,
		StartedAt: time.Now().UTC(),
	}
}

func StartContext(ctx context.Context, name, traceID, spanID, parentID string) (context.Context, *Span) {
	s := Start(name, traceID, spanID, parentID)
	return context.WithValue(ctx, ctxKey, s), s
}

func FromContext(ctx context.Context) (*Span, bool) {
	v, ok := ctx.Value(ctxKey).(*Span)
	return v, ok
}

func (s *Span) End() {
	if s == nil || !s.EndedAt.IsZero() {
		return
	}
	s.EndedAt = time.Now().UTC()
}

func (s *Span) Duration() time.Duration {
	if s == nil || s.EndedAt.IsZero() {
		return 0
	}
	return s.EndedAt.Sub(s.StartedAt)
}
