package mocks

import "context"

type Logger struct{ Entries []map[string]any }

func (l *Logger) Info(_ context.Context, fields map[string]any) {
	l.Entries = append(l.Entries, fields)
}
