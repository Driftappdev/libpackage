package cache

import "sync"

type Cache struct {
	mu   sync.RWMutex
	data map[string]bool
}

func New() *Cache { return &Cache{data: map[string]bool{}} }

func (c *Cache) Get(key string) (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[key]
	return v, ok
}
func (c *Cache) Set(key string, value bool) { c.mu.Lock(); defer c.mu.Unlock(); c.data[key] = value }
