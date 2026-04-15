package logger

import (
	"io"
	"log/slog"
	"os"
)

type Options struct {
	Level     slog.Leveler
	Output    io.Writer
	AddSource bool
}

func (o Options) normalize() Options {
	if o.Level == nil {
		o.Level = slog.LevelInfo
	}
	if o.Output == nil {
		o.Output = os.Stdout
	}
	return o
}
