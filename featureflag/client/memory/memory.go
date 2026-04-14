package client

import (
	"context"
	"sync"
)

// Flag is an in-memory feature-flag model used by the rich client.
type Flag struct {
	Key     string
	Enabled bool
	Variant string
	Rules   map[string]string
}

// StoreClient is an in-memory feature flag client.
type StoreClient struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

func NewStore() *StoreClient {
	return &StoreClient{flags: map[string]Flag{}}
}

func (c *StoreClient) Upsert(_ context.Context, flag Flag) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flags[flag.Key] = flag
}

func (c *StoreClient) Get(_ context.Context, key string) (Flag, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	flag, ok := c.flags[key]
	return flag, ok
}

func (c *StoreClient) Snapshot(_ context.Context) map[string]Flag {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]Flag, len(c.flags))
	for k, v := range c.flags {
		out[k] = v
	}
	return out
}
