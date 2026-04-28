package logger

import (
	"context"
	"log/slog"
)

type Logger interface {
	DebugContext(ctx context.Context, msg string, fields ...Field)
	InfoContext(ctx context.Context, msg string, fields ...Field)
	WarnContext(ctx context.Context, msg string, fields ...Field)
	ErrorContext(ctx context.Context, msg string, fields ...Field)
	With(fields ...Field) Logger
	Underlying() *slog.Logger
}

type stdLogger struct{ l *slog.Logger }

func New(opts Options) Logger {
	opts = opts.normalize()
	h := slog.NewJSONHandler(opts.Output, &slog.HandlerOptions{AddSource: opts.AddSource, Level: opts.Level})
	return stdLogger{l: slog.New(h)}
}

func FromSlog(l *slog.Logger) Logger {
	if l == nil {
		return New(Options{})
	}
	return stdLogger{l: l}
}

func (s stdLogger) DebugContext(ctx context.Context, msg string, fields ...Field) {
	s.l.DebugContext(ctx, msg, attrs(fields)...)
}
func (s stdLogger) InfoContext(ctx context.Context, msg string, fields ...Field) {
	s.l.InfoContext(ctx, msg, attrs(fields)...)
}
func (s stdLogger) WarnContext(ctx context.Context, msg string, fields ...Field) {
	s.l.WarnContext(ctx, msg, attrs(fields)...)
}
func (s stdLogger) ErrorContext(ctx context.Context, msg string, fields ...Field) {
	s.l.ErrorContext(ctx, msg, attrs(fields)...)
}
func (s stdLogger) With(fields ...Field) Logger { return stdLogger{l: s.l.With(attrs(fields)...)} }
func (s stdLogger) Underlying() *slog.Logger    { return s.l }

func attrs(fields []Field) []any {
	out := make([]any, 0, len(fields))
	for _, f := range fields {
		out = append(out, slog.Any(f.Key, f.Value))
	}
	return out
}
