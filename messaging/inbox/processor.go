package inbox

import (
    "context"
    "time"
)

type Processor struct {
    repo     Repository
    dedupe   DedupeStore
    handlers map[string]Handler
    options  Options
}

func NewProcessor(repo Repository, dedupe DedupeStore, opts ...Option) *Processor {
    if repo == nil { repo = NewMemoryRepository() }
    if dedupe == nil { dedupe = NewMemoryDedupe() }
    return &Processor{repo: repo, dedupe: dedupe, handlers: make(map[string]Handler), options: BuildOptions(opts...)}
}
func (p *Processor) Register(key string, h Handler, middlewares ...Middleware) { p.handlers[key] = Chain(h, middlewares...) }
func (p *Processor) Process(ctx context.Context, msg Message) error {
    seen, err := p.dedupe.Seen(ctx, msg.ID)
    if err != nil { return err }
    if seen { return ErrDuplicateMessage }
    if msg.ReceivedAt.IsZero() { msg.ReceivedAt = time.Now().UTC() }
    msg.Status = StatusReceived
    if err := p.repo.Save(ctx, msg); err != nil { return err }
    if err := p.repo.MarkProcessing(ctx, msg.ID); err != nil { return err }
    key := p.options.HandlerKeyFunc(msg)
    h, ok := p.handlers[key]
    if !ok { return ErrHandlerNotFound }
    if err := h.Handle(ctx, msg); err != nil { _ = p.repo.MarkFailed(ctx, msg.ID, err); return err }
    now := time.Now().UTC()
    if err := p.dedupe.Mark(ctx, msg.ID, now); err != nil { return err }
    return p.repo.MarkProcessed(ctx, msg.ID, now)
}
