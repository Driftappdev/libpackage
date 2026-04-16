package dlq

import "context"

type Inspector struct { store Store }

func NewInspector(store Store) *Inspector { if store == nil { store = NewMemoryStore() }; return &Inspector{store: store} }
func (i *Inspector) Get(ctx context.Context, id string) (Message, error) { return i.store.Get(ctx, id) }
func (i *Inspector) List(ctx context.Context, f Filter) ([]Message, error) { return i.store.List(ctx, f) }
