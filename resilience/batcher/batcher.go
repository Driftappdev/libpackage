package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BatcherConfig configures a Batcher.
type BatcherConfig[T any] struct {
	// Name identifies this batcher in metrics.
	Name string
	// MaxSize is the maximum number of items per batch (0 = unlimited).
	MaxSize int
	// MaxWait is the maximum time to wait before flushing a partial batch.
	MaxWait time.Duration
	// MaxBytes is an optional byte-size limit per batch (0 = disabled).
	// SizeOf must be set for this to work.
	MaxBytes int64
	// SizeOf returns the byte size of an item (optional).
	SizeOf func(item T) int64
	// FlushFn is called with each batch. Errors are returned to callers.
	FlushFn func(ctx context.Context, batch []T) error
	// OnFlush is called after each flush for observability.
	OnFlush func(name string, count int, duration time.Duration, err error)
	// OnItemDrop is called if an item is dropped.
	OnItemDrop func(name string, reason string)
}

// batchEntry holds a submitted item along with its reply channel.
type batchEntry[T any] struct {
	item   T
	doneCh chan error
}

// Batcher groups individual items into batches and flushes them via FlushFn.
// Useful for reducing per-item overhead (DB inserts, API calls, Kafka produce).
// Thread-safe. All submitters receive the batch error (fan-out error delivery).
type Batcher[T any] struct {
	cfg    BatcherConfig[T]
	entryCh chan batchEntry[T]
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once

	// metrics
	submitted atomic.Int64
	batches   atomic.Int64
	flushErrs atomic.Int64
}

// NewBatcher creates and starts a Batcher.
func NewBatcher[T any](cfg BatcherConfig[T]) *Batcher[T] {
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = 10 * time.Millisecond
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 500
	}
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Batcher[T]{
		cfg:     cfg,
		entryCh: make(chan batchEntry[T], cfg.MaxSize*2),
		ctx:     ctx,
		cancel:  cancel,
	}
	b.wg.Add(1)
	go b.run()
	return b
}

// Add submits an item to the batcher and blocks until the batch is flushed.
// Returns the flush error (nil = success).
func (b *Batcher[T]) Add(ctx context.Context, item T) error {
	doneCh := make(chan error, 1)
	entry := batchEntry[T]{item: item, doneCh: doneCh}

	select {
	case b.entryCh <- entry:
		b.submitted.Add(1)
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return fmt.Errorf("batcher[%s]: closed", b.cfg.Name)
	}

	select {
	case err := <-doneCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// AddAsync submits an item without waiting for the flush result.
// The returned channel will receive the error when the batch is flushed.
func (b *Batcher[T]) AddAsync(item T) (<-chan error, error) {
	doneCh := make(chan error, 1)
	select {
	case b.entryCh <- batchEntry[T]{item: item, doneCh: doneCh}:
		b.submitted.Add(1)
		return doneCh, nil
	default:
		return nil, fmt.Errorf("batcher[%s]: entry channel full", b.cfg.Name)
	}
}

func (b *Batcher[T]) run() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.cfg.MaxWait)
	defer ticker.Stop()

	var (
		batch    []batchEntry[T]
		byteSize int64
	)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		current := batch
		batch = nil
		byteSize = 0
		b.batches.Add(1)

		items := make([]T, len(current))
		for i, e := range current {
			items[i] = e.item
		}

		start := time.Now()
		err := b.cfg.FlushFn(b.ctx, items)
		dur := time.Since(start)

		if err != nil {
			b.flushErrs.Add(1)
		}
		if b.cfg.OnFlush != nil {
			b.cfg.OnFlush(b.cfg.Name, len(current), dur, err)
		}

		for _, e := range current {
			e.doneCh <- err
		}
	}

	for {
		select {
		case entry, ok := <-b.entryCh:
			if !ok {
				flush()
				return
			}
			batch = append(batch, entry)

			// Size tracking
			if b.cfg.SizeOf != nil {
				byteSize += b.cfg.SizeOf(entry.item)
			}

			// Flush conditions
			if len(batch) >= b.cfg.MaxSize ||
				(b.cfg.MaxBytes > 0 && byteSize >= b.cfg.MaxBytes) {
				flush()
				ticker.Reset(b.cfg.MaxWait)
			}

		case <-ticker.C:
			flush()

		case <-b.ctx.Done():
			flush()
			return
		}
	}
}

// Flush forces an immediate flush of any pending items.
// Blocks until flush completes.
func (b *Batcher[T]) Flush(ctx context.Context) error {
	// Submit a sentinel to force flush ordering
	sentinel := make(chan error, 1)
	b.entryCh <- batchEntry[T]{doneCh: sentinel}
	select {
	case err := <-sentinel:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close gracefully shuts down the batcher and flushes remaining items.
func (b *Batcher[T]) Close() {
	b.once.Do(func() {
		b.cancel()
		b.wg.Wait()
	})
}

// Stats returns a snapshot of batcher metrics.
func (b *Batcher[T]) Stats() BatcherStats {
	return BatcherStats{
		Name:      b.cfg.Name,
		Submitted: b.submitted.Load(),
		Batches:   b.batches.Load(),
		FlushErrs: b.flushErrs.Load(),
		Pending:   int64(len(b.entryCh)),
	}
}

// BatcherStats is a point-in-time snapshot.
type BatcherStats struct {
	Name      string
	Submitted int64
	Batches   int64
	FlushErrs int64
	Pending   int64
}
