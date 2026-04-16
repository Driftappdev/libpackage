package inbox

import "context"

type Handler interface { Handle(context.Context, Message) error }

type HandlerFunc func(context.Context, Message) error

func (f HandlerFunc) Handle(ctx context.Context, msg Message) error { return f(ctx, msg) }
