package ratelimit

import "time"

type Options struct {
	Policy Policy
	Store  Store
	Now    func() time.Time
}
