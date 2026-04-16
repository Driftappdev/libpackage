package outbox

import "context"

type Publisher interface { Publish(context.Context, Message) error }
