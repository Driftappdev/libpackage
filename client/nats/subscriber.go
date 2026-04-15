package nats

import "context"

type MessageHandler func(ctx context.Context, data []byte) error

type Subscriber interface {
	Subscribe(ctx context.Context, subject string, handler MessageHandler) error
}
