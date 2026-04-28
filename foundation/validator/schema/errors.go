package schema

import "errors"

var (
	ErrInvalidPayload = errors.New("invalid payload")
	ErrRuleViolation  = errors.New("rule violation")
)
