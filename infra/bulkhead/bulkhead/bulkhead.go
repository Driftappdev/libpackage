package bulkhead

import (
	"context"
	"errors"
)

var ErrRejected = errors.New("bulkhead rejected")

type Bulkhead struct{ sem chan struct{} }

func New(size int) *Bulkhead {
	if size <= 0 {
		size = 1
	}
	return &Bulkhead{sem: make(chan struct{}, size)}
}

func (b *Bulkhead) Do(ctx context.Context, fn func(context.Context) error) error {
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		return fn(ctx)
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrRejected
	}
}
