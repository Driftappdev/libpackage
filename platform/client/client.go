package gofeatureclient

import "context"

type Client interface {
	Enabled(ctx context.Context, key string, defaultValue bool) bool
}

type StaticClient struct{ Flags map[string]bool }

func (c StaticClient) Enabled(_ context.Context, key string, defaultValue bool) bool {
	if v, ok := c.Flags[key]; ok {
		return v
	}
	return defaultValue
}
