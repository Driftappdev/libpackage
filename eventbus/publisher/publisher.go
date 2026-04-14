package eventbus

import "context"

type Publisher interface {
	Publish(ctx context.Context, topic string, msg Envelope) error
}
