package deadletter

import (
	"context"

	"github.com/driftappdev/platform/eventbus/subscriber"
)

type Writer interface {
	Write(context.Context, subscriber.Message, error) error
}


