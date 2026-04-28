package resilience

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// WorkItem is a unit of work submitted to the WorkQueue.
type WorkItem[T any] struct {
	ID       string
	Payload  T
	Priority int // higher = more urgent (used with PriorityQueue integration)
	// ResultCh receives exactly one value: the work result.
	ResultCh chan WorkResult[T]
	// DeadlineCh is closed when the item deadline fires (optional).
	DeadlineCh <-chan struct{}
	enqueued   time.Time
}

// WorkResult wraps the output of a work function.
type WorkResult[T any] struct {
	Value    any
	Err      error
	Duration time.Duration
	WorkerID int
}

// WorkFunc is executed by worker goroutines.
type WorkFunc[T any] func(ctx context.Context, item WorkItem[T]) (any, error)

// WorkQueueConfig configures the work queue.
type WorkQueueConfig struct {
	// Workers is the number of goroutines to spawn.
	Workers int
	// QueueDepth is the channel buffer size. 0 = synchronous.
	QueueDepth int
	// WorkerIdleTimeout: workers exit if idle longer than this (dynamic scaling).
	WorkerIdleTimeout time.Duration
	// MaxWorkers caps dynamic worker count (0 = Workers is fixed).
	MaxWorkers int
	// DrainTimeout is how long Close() waits for in-flight work to finish.
	DrainTimeout time.Duration
	// PanicHandler is called if a worker panics. If nil, panic propagates.
	PanicHandler func(workerID int, v interface{})
	// OnEnqueue is called when an item is enqueued.
	OnEnqueue func(id string, queueLen int)
	// OnComplete is called when an item finishes.
	OnComplete func(id string, result WorkResult[any])
}

// WorkQueue is a bounded, multi-worker task queue with optional dynamic scaling,
// panic recovery, and graceful drain.
type WorkQueue[T any] struct {
	cfg      WorkQueueConfig
	fn       WorkFunc[T]
	queue    chan WorkItem[T]
	wg       sync.WaitGroup
	once     sync.Once
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex

	// metrics
	enqueued  atomic.Int64
	completed atomic.Int64
	failed    atomic.Int64
	panicked  atomic.Int64
	dropped   atomic.Int64
	workerCnt atomic.Int64
}

// NewWorkQueue creates and starts a WorkQueue.
func NewWorkQueue[T any](cfg WorkQueueConfig, fn WorkFunc[T]) *WorkQueue[T] {
	if cfg.Workers < 1 {
		cfg.Workers = runtime.NumCPU()
	}
	if cfg.QueueDepth < 0 {
		cfg.QueueDepth = 0
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	wq := &WorkQueue[T]{
		cfg:    cfg,
		fn:     fn,
		queue:  make(chan WorkItem[T], cfg.QueueDepth),
		ctx:    ctx,
		cancel: cancel,
	}
	for i := 0; i < cfg.Workers; i++ {
		wq.startWorker(i)
	}
	return wq
}

func (wq *WorkQueue[T]) startWorker(id int) {
	wq.wg.Add(1)
	wq.workerCnt.Add(1)
	go func() {
		defer wq.wg.Done()
		defer wq.workerCnt.Add(-1)
		wq.workerLoop(id)
	}()
}

func (wq *WorkQueue[T]) workerLoop(id int) {
	var idleTimer *time.Timer
	var idleCh <-chan time.Time

	if wq.cfg.WorkerIdleTimeout > 0 {
		idleTimer = time.NewTimer(wq.cfg.WorkerIdleTimeout)
		idleCh = idleTimer.C
		defer idleTimer.Stop()
	}

	for {
		select {
		case item, ok := <-wq.queue:
			if !ok {
				return
			}
			if idleTimer != nil {
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(wq.cfg.WorkerIdleTimeout)
			}
			wq.processItem(id, item)

		case <-idleCh:
			// Worker timed out idle — exit if we have more than minimum workers
			if wq.workerCnt.Load() > int64(wq.cfg.Workers) {
				return
			}
			// Reset timer for minimum workers
			idleTimer.Reset(wq.cfg.WorkerIdleTimeout)

		case <-wq.ctx.Done():
			return
		}
	}
}

func (wq *WorkQueue[T]) processItem(workerID int, item WorkItem[T]) {
	start := time.Now()
	var result WorkResult[any]
	result.WorkerID = workerID

	defer func() {
		if r := recover(); r != nil {
			wq.panicked.Add(1)
			result.Err = fmt.Errorf("worker %d panic: %v", workerID, r)
			if wq.cfg.PanicHandler != nil {
				wq.cfg.PanicHandler(workerID, r)
			}
		}
		result.Duration = time.Since(start)

		if result.Err != nil {
			wq.failed.Add(1)
		}
		wq.completed.Add(1)

		if item.ResultCh != nil {
			// Type-assert result value for typed channel
			typed := WorkResult[T]{
				Err:      result.Err,
				Duration: result.Duration,
				WorkerID: workerID,
			}
			select {
			case item.ResultCh <- typed:
			default:
			}
		}
		if wq.cfg.OnComplete != nil {
			wq.cfg.OnComplete(item.ID, result)
		}
	}()

	// Check item deadline
	if item.DeadlineCh != nil {
		select {
		case <-item.DeadlineCh:
			result.Err = fmt.Errorf("item %s: deadline exceeded before processing", item.ID)
			return
		default:
		}
	}

	val, err := wq.fn(wq.ctx, item)
	result.Value = val
	result.Err = err
}

// Submit enqueues work. Returns error if queue is full or closed.
func (wq *WorkQueue[T]) Submit(item WorkItem[T]) error {
	select {
	case <-wq.ctx.Done():
		return fmt.Errorf("resilience: work queue is closed")
	default:
	}

	item.enqueued = time.Now()
	if item.ResultCh == nil {
		item.ResultCh = make(chan WorkResult[T], 1)
	}

	select {
	case wq.queue <- item:
		wq.enqueued.Add(1)
		if wq.cfg.OnEnqueue != nil {
			wq.cfg.OnEnqueue(item.ID, len(wq.queue))
		}
		// Dynamic scale-out
		if wq.cfg.MaxWorkers > 0 && len(wq.queue) > cap(wq.queue)/2 &&
			int(wq.workerCnt.Load()) < wq.cfg.MaxWorkers {
			wq.startWorker(int(wq.workerCnt.Load()))
		}
		return nil

	default:
		wq.dropped.Add(1)
		return fmt.Errorf("resilience: work queue is full (%d items)", len(wq.queue))
	}
}

// SubmitWait submits and blocks until the work is done or ctx is cancelled.
func (wq *WorkQueue[T]) SubmitWait(ctx context.Context, item WorkItem[T]) (WorkResult[T], error) {
	if item.ResultCh == nil {
		item.ResultCh = make(chan WorkResult[T], 1)
	}
	if err := wq.Submit(item); err != nil {
		return WorkResult[T]{}, err
	}
	select {
	case <-ctx.Done():
		return WorkResult[T]{}, ctx.Err()
	case result := <-item.ResultCh:
		return result, result.Err
	}
}

// Close signals workers to stop and waits for drain.
func (wq *WorkQueue[T]) Close() error {
	wq.once.Do(func() {
		wq.cancel()
	})

	done := make(chan struct{})
	go func() {
		wq.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(wq.cfg.DrainTimeout):
		return fmt.Errorf("resilience: work queue drain timeout after %s", wq.cfg.DrainTimeout)
	}
}

// Stats returns a snapshot of queue metrics.
func (wq *WorkQueue[T]) Stats() WorkQueueStats {
	return WorkQueueStats{
		Enqueued:  wq.enqueued.Load(),
		Completed: wq.completed.Load(),
		Failed:    wq.failed.Load(),
		Panicked:  wq.panicked.Load(),
		Dropped:   wq.dropped.Load(),
		Workers:   wq.workerCnt.Load(),
		QueueLen:  int64(len(wq.queue)),
		QueueCap:  int64(cap(wq.queue)),
	}
}

// WorkQueueStats is a point-in-time snapshot.
type WorkQueueStats struct {
	Enqueued  int64
	Completed int64
	Failed    int64
	Panicked  int64
	Dropped   int64
	Workers   int64
	QueueLen  int64
	QueueCap  int64
}
