package outbox

import "context"

type Repository struct { store Store }

func NewRepository(store Store) *Repository { if store == nil { store = NewMemoryStore() }; return &Repository{store: store} }
func (r *Repository) Enqueue(ctx context.Context, msg Message) error { return r.store.Save(ctx, msg) }
func (r *Repository) Get(ctx context.Context, id string) (Message, error) { return r.store.Get(ctx, id) }
