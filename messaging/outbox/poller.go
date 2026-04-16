package outbox

import (
    "context"
    "time"
)

type Poller struct {
    dispatcher *Dispatcher
    options    Options
}

func NewPoller(dispatcher *Dispatcher, opts ...Option) *Poller { return &Poller{dispatcher: dispatcher, options: BuildOptions(opts...)} }
func (p *Poller) Run(ctx context.Context) error {
    ticker := time.NewTicker(p.options.PollInterval)
    defer ticker.Stop()
    for {
        if err := p.dispatcher.DispatchBatch(ctx); err != nil && p.options.StopOnError { return err }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
        }
    }
}
