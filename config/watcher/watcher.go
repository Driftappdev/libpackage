package config

import "time"

type WatchEvent struct {
	Path      string
	ChangedAt time.Time
}
