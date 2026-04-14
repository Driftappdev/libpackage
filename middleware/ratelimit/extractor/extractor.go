package ratelimit

import "net/http"

type KeyExtractor func(r *http.Request) string

func ByRemoteAddr(r *http.Request) string { return r.RemoteAddr }

func ByHeader(name string) KeyExtractor {
	return func(r *http.Request) string { return r.Header.Get(name) }
}
