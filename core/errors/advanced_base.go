package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

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

type Frame struct {
	Function string
	File     string
	Line     int
}

func (f Frame) String() string {
	return fmt.Sprintf("%s\n\t%s:%d", f.Function, f.File, f.Line)
}

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

type Meta map[string]any

type BaseError struct {
	Code     string
	Category Category
	Message  string
	Details  string
	meta     Meta
	stack    StackTrace
	cause    error
}

func NewBase(code string, category Category, message string) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  message,
		stack:    captureStack(1),
	}
}

func NewBasef(code string, category Category, format string, args ...any) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  fmt.Sprintf(format, args...),
		stack:    captureStack(1),
	}
}

func WrapBase(cause error, code string, category Category, message string) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  message,
		cause:    cause,
		stack:    captureStack(1),
	}
}

func WrapBasef(cause error, code string, category Category, format string, args ...any) *BaseError {
	return &BaseError{
		Code:     code,
		Category: category,
		Message:  fmt.Sprintf(format, args...),
		cause:    cause,
		stack:    captureStack(1),
	}
}

func (e *BaseError) WithDetails(details string) *BaseError {
	e.Details = details
	return e
}

func (e *BaseError) WithMeta(key string, value any) *BaseError {
	if e.meta == nil {
		e.meta = make(Meta)
	}
	e.meta[key] = value
	return e
}

func (e *BaseError) GetMeta() Meta      { return e.meta }
func (e *BaseError) Stack() StackTrace  { return e.stack }
func (e *BaseError) Unwrap() error      { return e.cause }
func (e *BaseError) HTTPStatus() int    { return e.Category.HTTPStatus() }
func (e *BaseError) HTTPCode() int      { return e.Category.HTTPStatus() }
func (e *BaseError) CategoryName() string { return string(e.Category) }

func (e *BaseError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *BaseError) Is(target error) bool {
	var t *BaseError
	if stderrors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

type APIResponse struct {
	Success bool      `json:"success"`
	Error   *APIError `json:"error,omitempty"`
}

type APIError struct {
	Code     string         `json:"code"`
	Category string         `json:"category"`
	Message  string         `json:"message"`
	Details  string         `json:"details,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
}

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

func NotFound(resource, id string) *BaseError {
	return NewBasef("NOT_FOUND", CategoryNotFound, "%s with id %q not found", resource, id)
}

func Unauthorized(reason string) *BaseError {
	return NewBase("UNAUTHORIZED", CategoryUnauthorized, reason)
}

func Forbidden(action string) *BaseError {
	return NewBasef("FORBIDDEN", CategoryForbidden, "access denied: %s", action)
}

func Validation(field, reason string) *BaseError {
	return NewBasef("VALIDATION_ERROR", CategoryValidation, "validation failed on %q: %s", field, reason)
}

func Internal(cause error) *BaseError {
	return WrapBase(cause, "INTERNAL_ERROR", CategoryInternal, "an internal error occurred")
}

func Conflict(resource, reason string) *BaseError {
	return NewBasef("CONFLICT", CategoryConflict, "%s conflict: %s", resource, reason)
}

func Timeout(op string) *BaseError {
	return NewBasef("TIMEOUT", CategoryTimeout, "operation %q timed out", op)
}

func RateLimit(limit int) *BaseError {
	return NewBasef("RATE_LIMIT_EXCEEDED", CategoryRateLimit, "rate limit of %d req/s exceeded", limit)
}

func IsCategory(err error, c Category) bool {
	var be *BaseError
	if stderrors.As(err, &be) {
		return be.Category == c
	}
	return false
}

func IsCode(err error, code string) bool {
	var be *BaseError
	if stderrors.As(err, &be) {
		return be.Code == code
	}
	return false
}

func HTTPStatusFrom(err error) int {
	var be *BaseError
	if stderrors.As(err, &be) {
		return be.HTTPStatus()
	}
	return http.StatusInternalServerError
}

type Aggregate struct {
	errs []error
}

func (a *Aggregate) Add(err error) {
	if err != nil {
		a.errs = append(a.errs, err)
	}
}

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
