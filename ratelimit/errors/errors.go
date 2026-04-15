package ratelimit

import (
	"errors"
	"fmt"
)

var (
	// Backward-compatible sentinels used by middleware and existing callers.
	ErrLimited   = errors.New("rate limited")
	ErrBadWindow = errors.New("invalid window")

	// Extended validation/runtime errors for production scenarios.
	ErrBadLimit         = errors.New("invalid limit")
	ErrBadCost          = errors.New("invalid cost")
	ErrBadBurst         = errors.New("invalid burst")
	ErrNilStore         = errors.New("nil store")
	ErrEmptyIdentity    = errors.New("empty key identity")
	ErrUnsupportedMode  = errors.New("unsupported rate-limit strategy")
	ErrRedisExecutorNil = errors.New("redis executor is nil")
	ErrStoreFailure     = errors.New("rate-limit store failure")
)

type ErrorCode string

const (
	CodeLimited         ErrorCode = "LIMITED"
	CodeInvalidPolicy   ErrorCode = "INVALID_POLICY"
	CodeInvalidKey      ErrorCode = "INVALID_KEY"
	CodeStoreFailure    ErrorCode = "STORE_FAILURE"
	CodeUnsupportedMode ErrorCode = "UNSUPPORTED_MODE"
	CodeConfiguration   ErrorCode = "CONFIGURATION"
	CodeInternal        ErrorCode = "INTERNAL"
)

// Error enriches ratelimit failures with contextual information.
type Error struct {
	Code      ErrorCode
	Op        string
	Key       string
	Policy    string
	Temporary bool
	Cause     error
	Message   string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := e.Message
	if msg == "" {
		msg = string(e.Code)
	}
	if e.Op != "" {
		msg = fmt.Sprintf("%s: %s", e.Op, msg)
	}
	if e.Key != "" {
		msg = fmt.Sprintf("%s (key=%s)", msg, e.Key)
	}
	if e.Cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Cause)
	}
	return msg
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}
	if target == ErrLimited && e != nil && e.Code == CodeLimited {
		return true
	}
	return errors.Is(e.Cause, target)
}

func WrapError(code ErrorCode, op, key, policy, msg string, cause error) error {
	return &Error{
		Code:    code,
		Op:      op,
		Key:     key,
		Policy:  policy,
		Message: msg,
		Cause:   cause,
	}
}

func IsLimited(err error) bool {
	return errors.Is(err, ErrLimited)
}
