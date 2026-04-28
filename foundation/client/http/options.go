package http

import "time"

type Options struct {
	Timeout   time.Duration
	UserAgent string
}
