package ratelimit

import "time"

type Result struct {
	Allowed   bool
	Limit     int64
	Remaining int64
	ResetAt   time.Time
}
