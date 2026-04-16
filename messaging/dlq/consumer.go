package dlq

import "context"

type Consumer struct { store Store }

func NewConsumer(store Store) *Consumer { if store == nil { store = NewMemoryStore() }; return &Consumer{store: store} }
func (c *Consumer) Consume(ctx context.Context, msg Message) error { return c.store.Put(ctx, msg) }
