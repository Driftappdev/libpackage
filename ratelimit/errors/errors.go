package ratelimit

import "errors"

var (
	ErrLimited   = errors.New("rate limited")
	ErrBadWindow = errors.New("invalid window")
)
