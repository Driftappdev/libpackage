package nats

import "context"

type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}
