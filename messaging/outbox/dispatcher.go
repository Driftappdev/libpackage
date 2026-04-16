package outbox

import "context"

type Dispatcher struct {
    store     Store
    processor *Processor
    options   Options
}

func NewDispatcher(store Store, publisher Publisher, opts ...Option) *Dispatcher {
    o := BuildOptions(opts...)
    if store == nil { store = NewMemoryStore() }
    return &Dispatcher{store: store, processor: NewProcessor(store, publisher, opts...), options: o}
}

func (d *Dispatcher) DispatchBatch(ctx context.Context) error {
    batch, err := d.store.ReserveBatch(ctx, d.options.BatchSize, d.options.ReserveFor)
    if err != nil { return err }
    for _, msg := range batch {
        if err := d.processor.Process(ctx, msg); err != nil && d.options.StopOnError { return err }
    }
    return nil
}
