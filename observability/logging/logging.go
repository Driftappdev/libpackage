package logging

import (
	"context"
	"log/slog"
	"os"
)

// Logger is the common logging abstraction for this module.
type Logger interface {
	DebugContext(context.Context, string, ...any)
	InfoContext(context.Context, string, ...any)
	WarnContext(context.Context, string, ...any)
	ErrorContext(context.Context, string, ...any)
	With(...any) Logger
}

type stdLogger struct {
	l *slog.Logger
}

func New() Logger {
	return stdLogger{l: slog.New(slog.NewJSONHandler(os.Stdout, nil))}
}

func FromSlog(l *slog.Logger) Logger {
	if l == nil {
		return New()
	}
	return stdLogger{l: l}
}

func (s stdLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	s.l.DebugContext(ctx, msg, args...)
}
func (s stdLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	s.l.InfoContext(ctx, msg, args...)
}
func (s stdLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	s.l.WarnContext(ctx, msg, args...)
}
func (s stdLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	s.l.ErrorContext(ctx, msg, args...)
}
func (s stdLogger) With(args ...any) Logger { return stdLogger{l: s.l.With(args...)} }
