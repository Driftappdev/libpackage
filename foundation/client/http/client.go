package http

import (
	"net/http"
	"time"
)

func New(opts Options) *http.Client {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: roundTripper{base: http.DefaultTransport, userAgent: opts.UserAgent}}
}

type roundTripper struct {
	base      http.RoundTripper
	userAgent string
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", r.userAgent)
	}
	return r.base.RoundTrip(req)
}
