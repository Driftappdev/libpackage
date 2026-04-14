package ratelimit

import (
	"context"
	"time"
)

type RedisStore struct{}

func NewRedisStore() *RedisStore { return &RedisStore{} }
func (s *RedisStore) Increment(_ context.Context, _ string, window time.Duration, now time.Time) (int64, time.Time, error) {
	return 1, now.Add(window), nil
}
