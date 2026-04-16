package inbox

import "context"

type Middleware func(Handler) Handler

func Chain(h Handler, middlewares ...Middleware) Handler {
    if h == nil { return HandlerFunc(func(context.Context, Message) error { return nil }) }
    for i := len(middlewares) - 1; i >= 0; i-- { if middlewares[i] != nil { h = middlewares[i](h) } }
    return h
}
