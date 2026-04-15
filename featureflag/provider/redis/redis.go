package provider

import "context"

type RedisClient interface {
	Get(context.Context, string) (string, error)
}

type Redis struct{ client RedisClient }

func NewRedis(client RedisClient) *Redis { return &Redis{client: client} }

func (r *Redis) Bool(ctx context.Context, key string, fallback bool) bool {
	if r == nil || r.client == nil {
		return fallback
	}
	v, err := r.client.Get(ctx, key)
	if err != nil {
		return fallback
	}
	return v == "true" || v == "1"
}
