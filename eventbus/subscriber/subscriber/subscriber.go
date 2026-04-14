package subscriber

import "context"

type Message struct {
	Topic   string
	Key     string
	Payload []byte
	Headers map[string]string
}

type Handler func(context.Context, Message) error

type Subscriber interface {
	Subscribe(ctx context.Context, topic string, handler Handler) error
}
