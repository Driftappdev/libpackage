package eventbus

import "context"

type Handler func(ctx context.Context, msg Envelope) error

type Subscriber interface {
	Subscribe(ctx context.Context, topic string, handler Handler) error
}
