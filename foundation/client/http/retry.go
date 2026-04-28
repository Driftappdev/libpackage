package http

import (
	"net/http"
	"time"
)

type RetryPolicy struct {
	MaxAttempts int
	Backoff     func(attempt int) time.Duration
	ShouldRetry func(resp *http.Response, err error) bool
}

func (p RetryPolicy) normalize() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	if p.Backoff == nil {
		p.Backoff = func(int) time.Duration { return 0 }
	}
	if p.ShouldRetry == nil {
		p.ShouldRetry = func(resp *http.Response, err error) bool {
			if err != nil {
				return true
			}
			return resp != nil && resp.StatusCode >= 500
		}
	}
	return p
}
