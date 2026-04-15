package secrets

import (
	"context"
	"errors"
	"os"
)

var ErrNotFound = errors.New("secret not found")

type Provider interface {
	Get(ctx context.Context, key string) (string, error)
}

type EnvProvider struct{}

func (EnvProvider) Get(_ context.Context, key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", ErrNotFound
	}
	return v, nil
}
