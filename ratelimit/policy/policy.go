package ratelimit

import "time"

type Policy struct {
	Name   string
	Limit  int64
	Window time.Duration
}
