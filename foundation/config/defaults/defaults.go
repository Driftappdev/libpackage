package config

import "time"

type Defaults struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	GracePeriod  time.Duration
}

func NewDefaults() Defaults {
	return Defaults{ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, GracePeriod: 15 * time.Second}
}
