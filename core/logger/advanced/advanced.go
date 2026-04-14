package logger

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

func ParseLevel(s string) (Level, error) {
	for lvl, name := range levelNames {
		if name == s {
			return lvl, nil
		}
	}
	return LevelOff, fmt.Errorf("logger: unknown level %q", s)
}

type Entry struct {
	Time      time.Time
	Level     Level
	Message   string
	Fields    []Field
	Caller    string
	RequestID string
}

type Formatter interface {
	Format(e *Entry) ([]byte, error)
}

type Hook interface {
	Levels() []Level
	Fire(e *Entry) error
}

type Sampler interface {
	Sample(e *Entry) bool
}

type RotateWriter struct {
	mu          sync.Mutex
	filename    string
	maxBytes    int64
	maxBackups  int
	currentSize int64
	file        *os.File
}

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
		_ = f.Close()
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
		_ = rw.file.Close()
	}
	dir := filepath.Dir(rw.filename)
	base := filepath.Base(rw.filename)
	for i := rw.maxBackups - 1; i >= 1; i-- {
		old := filepath.Join(dir, fmt.Sprintf("%s.%d", base, i))
		newName := filepath.Join(dir, fmt.Sprintf("%s.%d", base, i+1))
		_ = os.Rename(old, newName)
	}
	_ = os.Rename(rw.filename, filepath.Join(dir, base+".1"))
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

type JSONFormatter struct {
	TimestampFormat string
}

func (f *JSONFormatter) Format(e *Entry) ([]byte, error) {
	tf := f.TimestampFormat
	if tf == "" {
		tf = time.RFC3339Nano
	}
	m := map[string]any{
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
	for _, fld := range e.Fields {
		m[fld.Key] = fld.Value
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

type TextFormatter struct {
	TimestampFormat string
}

func (f *TextFormatter) Format(e *Entry) ([]byte, error) {
	tf := f.TimestampFormat
	if tf == "" {
		tf = "2006-01-02T15:04:05.000Z07:00"
	}
	buf := fmt.Sprintf("%s %-5s %s", e.Time.Format(tf), e.Level.String(), e.Message)
	for _, fld := range e.Fields {
		buf += fmt.Sprintf(" %s=%v", fld.Key, fld.Value)
	}
	if e.Caller != "" {
		buf += fmt.Sprintf(" caller=%s", e.Caller)
	}
	return []byte(buf + "\n"), nil
}

type AdvancedOptions struct {
	Level           Level
	Formatter       Formatter
	Output          io.Writer
	AddCaller       bool
	CallerSkip      int
	Hooks           []Hook
	Sampler         Sampler
	Fields          []Field
	RequestIDGetter func(context.Context) string
}

type AdvancedLogger struct {
	level           atomic.Int32
	formatter       Formatter
	output          io.Writer
	mu              sync.Mutex
	addCaller       bool
	callerSkip      int
	hooks           []Hook
	sampler         Sampler
	fields          []Field
	requestIDGetter func(context.Context) string
}

func NewAdvanced(opts AdvancedOptions) *AdvancedLogger {
	l := &AdvancedLogger{
		formatter:       opts.Formatter,
		output:          opts.Output,
		addCaller:       opts.AddCaller,
		callerSkip:      opts.CallerSkip,
		hooks:           opts.Hooks,
		sampler:         opts.Sampler,
		fields:          opts.Fields,
		requestIDGetter: opts.RequestIDGetter,
	}
	l.level.Store(int32(opts.Level))
	if l.formatter == nil {
		l.formatter = &JSONFormatter{}
	}
	if l.output == nil {
		l.output = os.Stdout
	}
	if l.requestIDGetter == nil {
		l.requestIDGetter = RequestIDFromContext
	}
	return l
}

func DefaultAdvanced() *AdvancedLogger {
	return NewAdvanced(AdvancedOptions{
		Level:     LevelInfo,
		Formatter: &JSONFormatter{},
		Output:    os.Stdout,
		AddCaller: true,
	})
}

func (l *AdvancedLogger) SetLevel(lvl Level) {
	l.level.Store(int32(lvl))
}

func (l *AdvancedLogger) With(fields ...Field) *AdvancedLogger {
	child := &AdvancedLogger{
		formatter:       l.formatter,
		output:          l.output,
		addCaller:       l.addCaller,
		callerSkip:      l.callerSkip,
		hooks:           l.hooks,
		sampler:         l.sampler,
		fields:          append(append([]Field{}, l.fields...), fields...),
		requestIDGetter: l.requestIDGetter,
	}
	child.level.Store(l.level.Load())
	return child
}

func (l *AdvancedLogger) log(ctx context.Context, lvl Level, msg string, fields []Field) {
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
	if ctx != nil && l.requestIDGetter != nil {
		e.RequestID = l.requestIDGetter(ctx)
	}
	if l.sampler != nil && !l.sampler.Sample(e) {
		return
	}
	b, err := l.formatter.Format(e)
	if err == nil {
		l.mu.Lock()
		_, _ = l.output.Write(b)
		l.mu.Unlock()
	}
	for _, h := range l.hooks {
		for _, hl := range h.Levels() {
			if hl == lvl {
				_ = h.Fire(e)
				break
			}
		}
	}
	if lvl == LevelFatal {
		os.Exit(1)
	}
}

func (l *AdvancedLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, LevelDebug, msg, fields)
}
func (l *AdvancedLogger) Info(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, LevelInfo, msg, fields)
}
func (l *AdvancedLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, LevelWarn, msg, fields)
}
func (l *AdvancedLogger) Error(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, LevelError, msg, fields)
}
func (l *AdvancedLogger) Fatal(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, LevelFatal, msg, fields)
}

func (l *AdvancedLogger) Debugf(ctx context.Context, format string, args ...any) {
	l.log(ctx, LevelDebug, fmt.Sprintf(format, args...), nil)
}
func (l *AdvancedLogger) Infof(ctx context.Context, format string, args ...any) {
	l.log(ctx, LevelInfo, fmt.Sprintf(format, args...), nil)
}
func (l *AdvancedLogger) Warnf(ctx context.Context, format string, args ...any) {
	l.log(ctx, LevelWarn, fmt.Sprintf(format, args...), nil)
}
func (l *AdvancedLogger) Errorf(ctx context.Context, format string, args ...any) {
	l.log(ctx, LevelError, fmt.Sprintf(format, args...), nil)
}
func (l *AdvancedLogger) Fatalf(ctx context.Context, format string, args ...any) {
	l.log(ctx, LevelFatal, fmt.Sprintf(format, args...), nil)
}

type requestIDKey struct{}

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if rid, ok := ctx.Value(requestIDKey{}).(string); ok {
		return rid
	}
	return ""
}

type RateSampler struct {
	mu       sync.Mutex
	rate     int
	interval time.Duration
	counts   map[Level]int
	reset    time.Time
}

func NewRateSampler(rate int, interval time.Duration) *RateSampler {
	return &RateSampler{
		rate:     rate,
		interval: interval,
		counts:   make(map[Level]int),
		reset:    time.Now().Add(interval),
	}
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

func MultiWriter(writers ...io.Writer) io.Writer {
	return io.MultiWriter(writers...)
}
