package ratelimit

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type Strategy string

const (
	StrategyFixedWindow   Strategy = "fixed_window"
	StrategySlidingWindow Strategy = "sliding_window"
	StrategyTokenBucket   Strategy = "token_bucket"
)

type Result struct {
	Allowed    bool
	Limit      int64
	Remaining  int64
	Used       int64
	ResetAt    time.Time
	RetryAfter time.Duration
	PolicyName string
	Strategy   Strategy
	ShadowMode bool
	Metadata   map[string]string
}

func (r Result) Headers() http.Header {
	h := make(http.Header)
	h.Set("X-RateLimit-Limit", strconv.FormatInt(r.Limit, 10))
	h.Set("X-RateLimit-Remaining", strconv.FormatInt(r.Remaining, 10))
	if !r.ResetAt.IsZero() {
		h.Set("X-RateLimit-Reset", strconv.FormatInt(r.ResetAt.Unix(), 10))
	}
	if r.RetryAfter > 0 {
		h.Set("Retry-After", strconv.FormatInt(int64(r.RetryAfter.Seconds()), 10))
	}
	if r.PolicyName != "" {
		h.Set("X-RateLimit-Policy", r.PolicyName)
	}
	if r.Strategy != "" {
		h.Set("X-RateLimit-Strategy", string(r.Strategy))
	}
	if r.ShadowMode {
		h.Set("X-RateLimit-Shadow", "1")
	}
	for k, v := range r.Metadata {
		if k == "" {
			continue
		}
		h.Set("X-RateLimit-Meta-"+k, v)
	}
	return h
}

func (r Result) Denied() bool {
	return !r.Allowed
}

func (r Result) String() string {
	return fmt.Sprintf(
		"allowed=%v limit=%d remaining=%d used=%d reset=%s strategy=%s policy=%s",
		r.Allowed, r.Limit, r.Remaining, r.Used, r.ResetAt.UTC().Format(time.RFC3339), r.Strategy, r.PolicyName,
	)
}
