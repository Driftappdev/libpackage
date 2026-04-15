// Package gologger provides a production-grade structured logging library
// with support for multiple output formats, log levels, hooks, sampling,
// context propagation, and log rotation.
package gologger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Level represents the severity of a log message.
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
	LevelOff
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
	LevelFatal: "FATAL",
}

func (l Level) String() string {
	if name, ok := levelNames[l]; ok {
		return name
	}
	return "UNKNOWN"
}

// ParseLevel parses a string level name into a Level.
func ParseLevel(s string) (Level, error) {
	for lvl, name := range levelNames {
		if name == s {
			return lvl, nil
		}
	}
	return LevelOff, fmt.Errorf("gologger: unknown level %q", s)
}

// Field represents a structured log field.
type Field struct {
	Key   string
	Value interface{}
}

// F creates a new Field.
func F(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// Entry is a single log entry before it is written.
type Entry struct {
	Time      time.Time
	Level     Level
	Message   string
	Fields    []Field
	Caller    string
	RequestID string
	TraceID   string
	SpanID    string
}

// Formatter serialises an Entry into bytes.
type Formatter interface {
	Format(e *Entry) ([]byte, error)
}

// Hook is called after each log entry is written.
type Hook interface {
	Levels() []Level
	Fire(e *Entry) error
}

// Sampler decides whether an entry should be emitted.
type Sampler interface {
	Sample(e *Entry) bool
}

// RotateWriter is an io.WriteCloser that rotates log files.
type RotateWriter struct {
	mu          sync.Mutex
	filename    string
	maxBytes    int64
	maxBackups  int
	currentSize int64
	file        *os.File
}

// NewRotateWriter creates a new RotateWriter.
func NewRotateWriter(filename string, maxMB int, maxBackups int) (*RotateWriter, error) {
	rw := &RotateWriter{
		filename:   filename,
		maxBytes:   int64(maxMB) * 1024 * 1024,
		maxBackups: maxBackups,
	}
	if err := rw.openOrCreate(); err != nil {
		return nil, err
	}
	return rw, nil
}

func (rw *RotateWriter) openOrCreate() error {
	f, err := os.OpenFile(rw.filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	rw.file = f
	rw.currentSize = info.Size()
	return nil
}

func (rw *RotateWriter) Write(p []byte) (n int, err error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.currentSize+int64(len(p)) > rw.maxBytes {
		if err := rw.rotate(); err != nil {
			return 0, err
		}
	}
	n, err = rw.file.Write(p)
	rw.currentSize += int64(n)
	return
}

func (rw *RotateWriter) rotate() error {
	if rw.file != nil {
		rw.file.Close()
	}
	dir := filepath.Dir(rw.filename)
	base := filepath.Base(rw.filename)
	for i := rw.maxBackups - 1; i >= 1; i-- {
		old := filepath.Join(dir, fmt.Sprintf("%s.%d", base, i))
		newName := filepath.Join(dir, fmt.Sprintf("%s.%d", base, i+1))
		os.Rename(old, newName)
	}
	os.Rename(rw.filename, filepath.Join(dir, base+".1"))
	return rw.openOrCreate()
}

func (rw *RotateWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.file != nil {
		return rw.file.Close()
	}
	return nil
}

// JSONFormatter formats entries as newline-delimited JSON.
type JSONFormatter struct {
	TimestampFormat string
}

func (f *JSONFormatter) Format(e *Entry) ([]byte, error) {
	tf := f.TimestampFormat
	if tf == "" {
		tf = time.RFC3339Nano
	}
	m := map[string]interface{}{
		"time":    e.Time.Format(tf),
		"level":   e.Level.String(),
		"message": e.Message,
	}
	if e.Caller != "" {
		m["caller"] = e.Caller
	}
	if e.RequestID != "" {
		m["request_id"] = e.RequestID
	}
	if e.TraceID != "" {
		m["trace_id"] = e.TraceID
	}
	if e.SpanID != "" {
		m["span_id"] = e.SpanID
	}
	for _, fld := range e.Fields {
		m[fld.Key] = fld.Value
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// TextFormatter formats entries as human-readable text.
type TextFormatter struct {
	TimestampFormat string
	NoColor         bool
}

var levelColors = map[Level]string{
	LevelDebug: "\033[36m",
	LevelInfo:  "\033[32m",
	LevelWarn:  "\033[33m",
	LevelError: "\033[31m",
	LevelFatal: "\033[35m",
}

const resetColor = "\033[0m"

func (f *TextFormatter) Format(e *Entry) ([]byte, error) {
	tf := f.TimestampFormat
	if tf == "" {
		tf = "2006-01-02T15:04:05.000Z07:00"
	}
	color := ""
	reset := ""
	if !f.NoColor {
		color = levelColors[e.Level]
		reset = resetColor
	}
	buf := fmt.Sprintf("%s%s %-5s%s %s", color, e.Time.Format(tf), e.Level.String(), reset, e.Message)
	for _, fld := range e.Fields {
		buf += fmt.Sprintf(" %s=%v", fld.Key, fld.Value)
	}
	if e.Caller != "" {
		buf += fmt.Sprintf(" caller=%s", e.Caller)
	}
	return []byte(buf + "\n"), nil
}

// contextKey is unexported to avoid collisions.
type contextKey struct{}

// Options configures a Logger.
type Options struct {
	Level       Level
	Formatter   Formatter
	Output      io.Writer
	AddCaller   bool
	CallerSkip  int
	Hooks       []Hook
	Sampler     Sampler
	Development bool
	Fields      []Field
}

// Logger is the main logging object.
type Logger struct {
	level      atomic.Int32
	formatter  Formatter
	output     io.Writer
	mu         sync.Mutex
	addCaller  bool
	callerSkip int
	hooks      []Hook
	sampler    Sampler
	fields     []Field
}

// New creates a new Logger with the given options.
func New(opts Options) *Logger {
	l := &Logger{
		formatter:  opts.Formatter,
		output:     opts.Output,
		addCaller:  opts.AddCaller,
		callerSkip: opts.CallerSkip,
		hooks:      opts.Hooks,
		sampler:    opts.Sampler,
		fields:     opts.Fields,
	}
	l.level.Store(int32(opts.Level))
	if l.formatter == nil {
		l.formatter = &JSONFormatter{}
	}
	if l.output == nil {
		l.output = os.Stdout
	}
	return l
}

// Default returns a sensible default logger writing JSON to stdout.
func Default() *Logger {
	return New(Options{
		Level:     LevelInfo,
		Formatter: &JSONFormatter{},
		Output:    os.Stdout,
		AddCaller: true,
	})
}

// SetLevel atomically changes the log level.
func (l *Logger) SetLevel(lvl Level) {
	l.level.Store(int32(lvl))
}

// GetLevel returns the current log level.
func (l *Logger) GetLevel() Level {
	return Level(l.level.Load())
}

// With returns a child logger with additional persistent fields.
func (l *Logger) With(fields ...Field) *Logger {
	child := &Logger{
		formatter:  l.formatter,
		output:     l.output,
		addCaller:  l.addCaller,
		callerSkip: l.callerSkip,
		hooks:      l.hooks,
		sampler:    l.sampler,
		fields:     append(append([]Field{}, l.fields...), fields...),
	}
	child.level.Store(l.level.Load())
	return child
}

// WithContext extracts trace/request metadata from ctx and returns a child logger.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	child := l.With()
	if rid, ok := ctx.Value(contextKey{}).(string); ok && rid != "" {
		child.fields = append(child.fields, F("request_id", rid))
	}
	return child
}

// ContextWithRequestID stores a request ID in ctx for later retrieval.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, contextKey{}, requestID)
}

func (l *Logger) log(lvl Level, msg string, fields []Field) {
	if Level(l.level.Load()) > lvl {
		return
	}
	e := &Entry{
		Time:    time.Now(),
		Level:   lvl,
		Message: msg,
		Fields:  append(append([]Field{}, l.fields...), fields...),
	}
	if l.addCaller {
		_, file, line, ok := runtime.Caller(2 + l.callerSkip)
		if ok {
			e.Caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
		}
	}
	if l.sampler != nil && !l.sampler.Sample(e) {
		return
	}
	b, err := l.formatter.Format(e)
	if err == nil {
		l.mu.Lock()
		l.output.Write(b)
		l.mu.Unlock()
	}
	for _, h := range l.hooks {
		for _, hl := range h.Levels() {
			if hl == lvl {
				h.Fire(e)
				break
			}
		}
	}
	if lvl == LevelFatal {
		os.Exit(1)
	}
}

func (l *Logger) Debug(msg string, fields ...Field) { l.log(LevelDebug, msg, fields) }
func (l *Logger) Info(msg string, fields ...Field)  { l.log(LevelInfo, msg, fields) }
func (l *Logger) Warn(msg string, fields ...Field)  { l.log(LevelWarn, msg, fields) }
func (l *Logger) Error(msg string, fields ...Field) { l.log(LevelError, msg, fields) }
func (l *Logger) Fatal(msg string, fields ...Field) { l.log(LevelFatal, msg, fields) }

// Debugf logs a debug message with fmt.Sprintf formatting.
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(LevelDebug, fmt.Sprintf(format, args...), nil)
}

// Infof logs an info message with fmt.Sprintf formatting.
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(LevelInfo, fmt.Sprintf(format, args...), nil)
}

// Warnf logs a warn message with fmt.Sprintf formatting.
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(LevelWarn, fmt.Sprintf(format, args...), nil)
}

// Errorf logs an error message with fmt.Sprintf formatting.
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(LevelError, fmt.Sprintf(format, args...), nil)
}

// Fatalf logs a fatal message and exits the process.
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.log(LevelFatal, fmt.Sprintf(format, args...), nil)
}

// RateSampler emits at most n entries per interval per level.
type RateSampler struct {
	mu       sync.Mutex
	rate     int
	interval time.Duration
	counts   map[Level]int
	reset    time.Time
}

// NewRateSampler creates a sampler allowing rate entries per interval.
func NewRateSampler(rate int, interval time.Duration) *RateSampler {
	return &RateSampler{rate: rate, interval: interval, counts: make(map[Level]int), reset: time.Now().Add(interval)}
}

func (s *RateSampler) Sample(e *Entry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.After(s.reset) {
		s.counts = make(map[Level]int)
		s.reset = now.Add(s.interval)
	}
	s.counts[e.Level]++
	return s.counts[e.Level] <= s.rate
}

// MultiWriter writes to multiple outputs simultaneously.
func MultiWriter(writers ...io.Writer) io.Writer {
	return io.MultiWriter(writers...)
}
