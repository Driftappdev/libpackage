// Package gologmid provides production-grade HTTP logging middleware:
// structured request/response logging, latency tracking, request ID injection,
// body capture, configurable field extraction, and skip patterns.
package gologmid

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ---- Request ID -------------------------------------------------------------

type requestIDKey struct{}

// RequestIDFromContext returns the request ID stored in ctx, or empty string.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- Logger interface -------------------------------------------------------

// Logger is the minimal interface expected by the middleware.
// Compatible with gologger.Logger and any structured logger.
type Logger interface {
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
}

// Field is a key-value log field.
type Field struct {
	Key   string
	Value interface{}
}

// F creates a Field.
func F(key string, value interface{}) Field { return Field{Key: key, Value: value} }

// ---- Response writer wrapper ------------------------------------------------

type responseWriter struct {
	http.ResponseWriter
	status      int
	bytesWritten int64
	body         *bytes.Buffer
	captureBody  bool
}

func newResponseWriter(w http.ResponseWriter, captureBody bool) *responseWriter {
	rw := &responseWriter{ResponseWriter: w, status: http.StatusOK, captureBody: captureBody}
	if captureBody {
		rw.body = &bytes.Buffer{}
	}
	return rw
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	if rw.captureBody && rw.body != nil {
		rw.body.Write(b[:n])
	}
	return n, err
}

// Unwrap supports http.ResponseController.
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// ---- Config -----------------------------------------------------------------

// Config configures the logging middleware.
type Config struct {
	// Logger to write to. Required.
	Logger Logger

	// RequestIDHeader is the header to read/write request IDs.
	// Default: "X-Request-Id"
	RequestIDHeader string

	// SkipPaths is a list of URL paths that will not be logged.
	SkipPaths []string

	// SkipIf is a custom predicate; if it returns true, the request is not logged.
	SkipIf func(r *http.Request) bool

	// CaptureRequestBody stores the request body in log fields (up to MaxBodyBytes).
	CaptureRequestBody bool

	// CaptureResponseBody stores the response body in log fields (up to MaxBodyBytes).
	CaptureResponseBody bool

	// MaxBodyBytes limits captured body size. Default: 4096.
	MaxBodyBytes int64

	// SlowRequestThreshold marks requests slow when latency exceeds this.
	SlowRequestThreshold time.Duration

	// ExtraFields adds additional fields to every log entry.
	ExtraFields []Field

	// FieldExtractor extracts additional fields from the request.
	FieldExtractor func(r *http.Request) []Field

	// LogLevel lets callers control which HTTP status codes log at WARN/ERROR.
	// Default: 4xx → Warn, 5xx → Error.
	LogLevel func(statusCode int) string // "info", "warn", "error"

	// SensitiveHeaders are redacted in logs (default: Authorization, Cookie).
	SensitiveHeaders []string
}

func (c *Config) defaults() {
	if c.RequestIDHeader == "" {
		c.RequestIDHeader = "X-Request-Id"
	}
	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = 4096
	}
	if c.SlowRequestThreshold == 0 {
		c.SlowRequestThreshold = time.Second
	}
	if len(c.SensitiveHeaders) == 0 {
		c.SensitiveHeaders = []string{"Authorization", "Cookie", "Set-Cookie"}
	}
}

// ---- Middleware -------------------------------------------------------------

// New returns a production-grade logging middleware.
func New(cfg Config) func(http.Handler) http.Handler {
	cfg.defaults()
	skip := make(map[string]bool, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skip[p] = true
	}
	sensitive := make(map[string]bool, len(cfg.SensitiveHeaders))
	for _, h := range cfg.SensitiveHeaders {
		sensitive[strings.ToLower(h)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logic.
			if skip[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			if cfg.SkipIf != nil && cfg.SkipIf(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Request ID.
			reqID := r.Header.Get(cfg.RequestIDHeader)
			if reqID == "" {
				reqID = newRequestID()
			}
			w.Header().Set(cfg.RequestIDHeader, reqID)
			ctx := context.WithValue(r.Context(), requestIDKey{}, reqID)
			r = r.WithContext(ctx)

			start := time.Now()

			// Capture request body.
			var reqBody string
			if cfg.CaptureRequestBody && r.Body != nil {
				lr := io.LimitReader(r.Body, cfg.MaxBodyBytes)
				bodyBytes, _ := io.ReadAll(lr)
				reqBody = sanitizeBody(bodyBytes)
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			// Wrap response writer.
			rw := newResponseWriter(w, cfg.CaptureResponseBody)

			// Serve.
			next.ServeHTTP(rw, r)

			latency := time.Since(start)

			// Build log fields.
			fields := []Field{
				F("request_id", reqID),
				F("method", r.Method),
				F("path", r.URL.Path),
				F("query", r.URL.RawQuery),
				F("status", rw.status),
				F("latency_ms", float64(latency.Microseconds())/1000.0),
				F("bytes", rw.bytesWritten),
				F("ip", clientIP(r)),
				F("user_agent", r.UserAgent()),
				F("protocol", r.Proto),
			}

			if latency >= cfg.SlowRequestThreshold {
				fields = append(fields, F("slow", true))
			}
			if reqBody != "" {
				fields = append(fields, F("req_body", reqBody))
			}
			if cfg.CaptureResponseBody && rw.body != nil {
				fields = append(fields, F("resp_body", sanitizeBody(rw.body.Bytes())))
			}
			if r.Referer() != "" {
				fields = append(fields, F("referer", r.Referer()))
			}
			if cfg.FieldExtractor != nil {
				fields = append(fields, cfg.FieldExtractor(r)...)
			}
			fields = append(fields, cfg.ExtraFields...)

			// Determine log level.
			level := "info"
			if cfg.LogLevel != nil {
				level = cfg.LogLevel(rw.status)
			} else {
				if rw.status >= 500 {
					level = "error"
				} else if rw.status >= 400 {
					level = "warn"
				}
			}

			msg := fmt.Sprintf("%s %s %d", r.Method, r.URL.Path, rw.status)
			switch level {
			case "error":
				cfg.Logger.Error(msg, fields...)
			case "warn":
				cfg.Logger.Warn(msg, fields...)
			default:
				cfg.Logger.Info(msg, fields...)
			}
		})
	}
}

// ---- Request ID middleware (standalone) -------------------------------------

// RequestID injects a request ID into the context and response headers.
// Use this when you only need request IDs without full request logging.
func RequestID(header string) func(http.Handler) http.Handler {
	if header == "" {
		header = "X-Request-Id"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(header)
			if id == "" {
				id = newRequestID()
			}
			w.Header().Set(header, id)
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ---- Recovery middleware ----------------------------------------------------

// Recovery catches panics, logs them, and returns a 500.
func Recovery(logger Logger, extraFields ...Field) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					fields := append([]Field{
						F("panic", fmt.Sprint(rec)),
						F("path", r.URL.Path),
						F("method", r.Method),
						F("request_id", RequestIDFromContext(r.Context())),
					}, extraFields...)
					logger.Error("panic recovered", fields...)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---- Helpers ----------------------------------------------------------------

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	if real := r.Header.Get("X-Real-Ip"); real != "" {
		return real
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		return r.RemoteAddr
	}
	return ip
}

func sanitizeBody(b []byte) string {
	if json.Valid(b) {
		return string(b)
	}
	// If not valid JSON, return truncated string.
	s := string(b)
	if len(s) > 512 {
		s = s[:512] + "…"
	}
	return s
}

// ---- Structured log adapter (stdlib-compatible) ----------------------------

// PrintLogger wraps fmt.Printf to satisfy the Logger interface (useful in tests).
type PrintLogger struct{}

func (PrintLogger) Info(msg string, fields ...Field)  { printLog("INFO", msg, fields) }
func (PrintLogger) Warn(msg string, fields ...Field)  { printLog("WARN", msg, fields) }
func (PrintLogger) Error(msg string, fields ...Field) { printLog("ERROR", msg, fields) }

func printLog(level, msg string, fields []Field) {
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "[%s] %s", level, msg)
	for _, f := range fields {
		fmt.Fprintf(sb, " %s=%v", f.Key, f.Value)
	}
	fmt.Println(sb.String())
}
