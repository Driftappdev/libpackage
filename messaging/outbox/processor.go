package outbox

import (
    "context"
    "time"
)

type Processor struct {
    store     Store
    publisher Publisher
    hooks     Hooks
    options   Options
}

func NewProcessor(store Store, publisher Publisher, opts ...Option) *Processor {
    if store == nil { store = NewMemoryStore() }
    return &Processor{store: store, publisher: publisher, options: BuildOptions(opts...)}
}
func (p *Processor) WithHooks(h Hooks) *Processor { p.hooks = h; return p }
func (p *Processor) Process(ctx context.Context, msg Message) error {
    if p.hooks.BeforeDispatch != nil { if err := p.hooks.BeforeDispatch(ctx, msg); err != nil { return err } }
    if err := p.publisher.Publish(ctx, msg); err != nil {
        if msg.Attempt+1 >= max(1, nonzero(msg.MaxAttempts, p.options.MaxAttempts)) {
            _ = p.store.MarkDeadLetter(ctx, msg.ID, err)
            if p.hooks.OnFailure != nil { _ = p.hooks.OnFailure(ctx, msg, err) }
            return ErrDispatchExhausted
        }
        next := time.Now().UTC().Add(backoff(msg.Attempt + 1))
        _ = p.store.MarkFailed(ctx, msg.ID, err, next, msg.Attempt+1)
        if p.hooks.OnFailure != nil { _ = p.hooks.OnFailure(ctx, msg, err) }
        return err
    }
    at := time.Now().UTC()
    if err := p.store.MarkPublished(ctx, msg.ID, at); err != nil { return err }
    if p.hooks.AfterDispatch != nil { return p.hooks.AfterDispatch(ctx, msg) }
    return nil
}
func backoff(attempt int) time.Duration { if attempt < 1 { attempt = 1 }; return time.Duration(attempt*attempt) * time.Second }
func nonzero(a, b int) int { if a > 0 { return a }; return b }
func max(a, b int) int { if a > b { return a }; return b }
