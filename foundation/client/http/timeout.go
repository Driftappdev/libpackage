package http

import "time"

func WithTimeout(opts Options, timeout time.Duration) Options {
	opts.Timeout = timeout
	return opts
}
