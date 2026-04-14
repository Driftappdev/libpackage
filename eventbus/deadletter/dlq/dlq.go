package deadletter

import (
	"context"

	"github.com/driftappdev/libpackage/eventbus/subscriber"
)

type Writer interface {
	Write(context.Context, subscriber.Message, error) error
}
