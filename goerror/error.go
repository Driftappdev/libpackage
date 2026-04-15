// Package goerror provides structured, categorised errors with stack traces,
// HTTP status mapping, error codes, and wrapping — production-ready for APIs.
package goerror

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// Category groups errors by domain so callers can branch without string matching.
type Category string

const (
	CategoryValidation   Category = "VALIDATION"
	CategoryNotFound     Category = "NOT_FOUND"
	CategoryUnauthorized Category = "UNAUTHORIZED"
	CategoryForbidden    Category = "FORBIDDEN"
	CategoryConflict     Category = "CONFLICT"
	CategoryInternal     Category = "INTERNAL"
	CategoryExternal     Category = "EXTERNAL"
	CategoryTimeout      Category = "TIMEOUT"
	CategoryRateLimit    Category = "RATE_LIMIT"
	CategoryUnavailable  Category = "UNAVAILABLE"
)

// HTTPStatus returns the canonical HTTP status code for the category.
func (c Category) HTTPStatus() int {
	switch c {
	case CategoryValidation:
		return http.StatusUnprocessableEntity
	case CategoryNotFound:
		return http.StatusNotFound
	case CategoryUnauthorized:
		return http.StatusUnauthorized
	case CategoryForbidden:
		return http.StatusForbidden
	case CategoryConflict:
		return http.StatusConflict
	case CategoryTimeout:
		return http.StatusGatewayTimeout
	case CategoryRateLimit:
		return http.StatusTooManyRequests
	case CategoryUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Frame is a single call-stack frame.
type Frame struct {
	Function string
	File     string
	Line     int
}

func (f Frame) String() string {
	return fmt.Sprintf("%s\n\t%s:%d", f.Function, f.File, f.Line)
}

// StackTrace is an ordered slice of frames (newest first).
type StackTrace []Frame

func (st StackTrace) String() string {
	sb := &strings.Builder{}
	for _, f := range st {
		sb.WriteString(f.String())
		sb.WriteByte('\n')
	}
	return sb.String()
}

func captureStack(skip int) StackTrace {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(skip+2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var st StackTrace
	for {
		f, more := frames.Next()
		st = append(st, Frame{Function: f.Function, File: f.File, Line: f.Line})
		if !more {
			break
		}
	}
	return st
}

// Meta is arbitrary key-value data attached to an error.
type Meta map[string]interface{}

// BaseError is the foundational error type for the library.
type BaseError struct {
	Code     string
	Category Category
	Message  string
	Details  string
	meta     Meta
	stack    StackTrace
	cause    error
}

// New constructs a BaseError capturing the call stack.
func New(code string, category Category, message string) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  message,
		stack:    captureStack(1),
	}
}

// Newf constructs a BaseError with a formatted message.
func Newf(code string, category Category, format string, args ...interface{}) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  fmt.Sprintf(format, args...),
		stack:    captureStack(1),
	}
}

// Wrap wraps a cause error in a BaseError, preserving the original stack when possible.
func Wrap(cause error, code string, category Category, message string) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  message,
		cause:    cause,
		stack:    captureStack(1),
	}
}

// Wrapf wraps with a formatted message.
func Wrapf(cause error, code string, category Category, format string, args ...interface{}) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  fmt.Sprintf(format, args...),
		cause:    cause,
		stack:    captureStack(1),
	}
}

// WithDetails attaches a human-readable detail string (safe for external consumers).
func (e *BaseError) WithDetails(details string) *BaseError {
	e.Details = details
	return e
}

// WithMeta attaches arbitrary structured metadata.
func (e *BaseError) WithMeta(key string, value interface{}) *BaseError {
	if e.meta == nil {
		e.meta = make(Meta)
	}
	e.meta[key] = value
	return e
}

// GetMeta returns the metadata map.
func (e *BaseError) GetMeta() Meta { return e.meta }

// Stack returns the captured stack trace.
func (e *BaseError) Stack() StackTrace { return e.stack }

// Unwrap implements the errors.Unwrap interface.
func (e *BaseError) Unwrap() error { return e.cause }

// Error implements the error interface.
func (e *BaseError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// HTTPStatus returns the HTTP status code corresponding to this error's category.
func (e *BaseError) HTTPStatus() int { return e.Category.HTTPStatus() }

// Is supports errors.Is for code-based matching.
func (e *BaseError) Is(target error) bool {
	var t *BaseError
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// APIResponse is a serialisable error payload for REST APIs.
type APIResponse struct {
	Success bool      `json:"success"`
	Error   *APIError `json:"error,omitempty"`
}

// APIError is the nested error object in APIResponse.
type APIError struct {
	Code     string                 `json:"code"`
	Category string                 `json:"category"`
	Message  string                 `json:"message"`
	Details  string                 `json:"details,omitempty"`
	Meta     map[string]interface{} `json:"meta,omitempty"`
}

// ToAPIResponse converts a BaseError to a serialisable API response.
func ToAPIResponse(e *BaseError) APIResponse {
	return APIResponse{
		Success: false,
		Error: &APIError{
			Code:     e.Code,
			Category: string(e.Category),
			Message:  e.Message,
			Details:  e.Details,
			Meta:     e.meta,
		},
	}
}

// ---- Convenience constructors ------------------------------------------------

func NotFound(resource, id string) *BaseError {
	return Newf("NOT_FOUND", CategoryNotFound, "%s with id %q not found", resource, id)
}

func Unauthorized(reason string) *BaseError {
	return New("UNAUTHORIZED", CategoryUnauthorized, reason)
}

func Forbidden(action string) *BaseError {
	return Newf("FORBIDDEN", CategoryForbidden, "access denied: %s", action)
}

func Validation(field, reason string) *BaseError {
	return Newf("VALIDATION_ERROR", CategoryValidation, "validation failed on %q: %s", field, reason)
}

func Internal(cause error) *BaseError {
	return Wrap(cause, "INTERNAL_ERROR", CategoryInternal, "an internal error occurred")
}

func Conflict(resource, reason string) *BaseError {
	return Newf("CONFLICT", CategoryConflict, "%s conflict: %s", resource, reason)
}

func Timeout(op string) *BaseError {
	return Newf("TIMEOUT", CategoryTimeout, "operation %q timed out", op)
}

func RateLimit(limit int) *BaseError {
	return Newf("RATE_LIMIT_EXCEEDED", CategoryRateLimit, "rate limit of %d req/s exceeded", limit)
}

// ---- Helpers -----------------------------------------------------------------

// IsCategory reports whether err's category matches c.
func IsCategory(err error, c Category) bool {
	var be *BaseError
	if errors.As(err, &be) {
		return be.Category == c
	}
	return false
}

// IsCode reports whether err has the given error code.
func IsCode(err error, code string) bool {
	var be *BaseError
	if errors.As(err, &be) {
		return be.Code == code
	}
	return false
}

// HTTPStatusFrom extracts the HTTP status code from any error.
// Falls back to 500 for unknown error types.
func HTTPStatusFrom(err error) int {
	var be *BaseError
	if errors.As(err, &be) {
		return be.HTTPStatus()
	}
	return http.StatusInternalServerError
}

// Aggregate collects multiple errors into a single error value.
type Aggregate struct {
	errs []error
}

// Add appends an error (nil is silently ignored).
func (a *Aggregate) Add(err error) {
	if err != nil {
		a.errs = append(a.errs, err)
	}
}

// Err returns the aggregate as an error, or nil if empty.
func (a *Aggregate) Err() error {
	if len(a.errs) == 0 {
		return nil
	}
	return a
}

func (a *Aggregate) Error() string {
	msgs := make([]string, len(a.errs))
	for i, e := range a.errs {
		msgs[i] = e.Error()
	}
	return fmt.Sprintf("%d errors: [%s]", len(a.errs), strings.Join(msgs, "; "))
}

func (a *Aggregate) Unwrap() []error { return a.errs }
