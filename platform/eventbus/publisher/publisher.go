package publisher

import "context"

type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte, headers map[string]string) error
}
