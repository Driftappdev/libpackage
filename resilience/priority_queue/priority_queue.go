package resilience

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PQItem is an item in the priority queue.
type PQItem[T any] struct {
	Value    T
	Priority int       // higher value = higher priority
	EnqueuedAt time.Time
	index    int       // maintained by heap.Interface
}

// pqHeap is the internal heap implementing heap.Interface.
type pqHeap[T any] []*PQItem[T]

func (h pqHeap[T]) Len() int { return len(h) }
func (h pqHeap[T]) Less(i, j int) bool {
	if h[i].Priority != h[j].Priority {
		return h[i].Priority > h[j].Priority // max-heap by priority
	}
	return h[i].EnqueuedAt.Before(h[j].EnqueuedAt) // FIFO within priority
}
func (h pqHeap[T]) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *pqHeap[T]) Push(x any) {
	n := len(*h)
	item := x.(*PQItem[T])
	item.index = n
	*h = append(*h, item)
}
func (h *pqHeap[T]) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

// PriorityQueueConfig configures a PriorityQueue.
type PriorityQueueConfig struct {
	// Name identifies this queue.
	Name string
	// MaxSize is the maximum number of items (0 = unlimited).
	MaxSize int
	// EvictLowest: when full, evict the lowest-priority item instead of rejecting.
	EvictLowest bool
	// OnEvict is called when an item is evicted.
	OnEvict func(name string, priority int)
	// OnEnqueue is called when an item is added.
	OnEnqueue func(name string, size int, priority int)
	// OnDequeue is called when an item is removed.
	OnDequeue func(name string, size int, priority int, waited time.Duration)
}

// PriorityQueue is a thread-safe, blocking, bounded priority queue.
// Items with the same priority are dequeued FIFO.
// It integrates with the resilience library and supports dynamic re-prioritization.
type PriorityQueue[T any] struct {
	cfg  PriorityQueueConfig
	h    pqHeap[T]
	mu   sync.Mutex
	cond *sync.Cond

	enqueued  atomic.Int64
	dequeued  atomic.Int64
	evicted   atomic.Int64
	rejected  atomic.Int64
}

// NewPriorityQueue creates and initializes a PriorityQueue.
func NewPriorityQueue[T any](cfg PriorityQueueConfig) *PriorityQueue[T] {
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	pq := &PriorityQueue[T]{cfg: cfg}
	pq.cond = sync.NewCond(&pq.mu)
	heap.Init(&pq.h)
	return pq
}

// Enqueue adds an item with the given priority.
// Returns an error if the queue is full and EvictLowest is false.
func (pq *PriorityQueue[T]) Enqueue(item T, priority int) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if pq.cfg.MaxSize > 0 && pq.h.Len() >= pq.cfg.MaxSize {
		if pq.cfg.EvictLowest {
			// Find and remove the lowest priority item
			lowest := pq.findLowest()
			if lowest != nil && lowest.Priority < priority {
				heap.Remove(&pq.h, lowest.index)
				pq.evicted.Add(1)
				if pq.cfg.OnEvict != nil {
					pq.cfg.OnEvict(pq.cfg.Name, lowest.Priority)
				}
			} else {
				// New item is not better than the lowest; reject it
				pq.rejected.Add(1)
				return fmt.Errorf("pqueue[%s]: full and new item priority %d <= lowest %d",
					pq.cfg.Name, priority, lowest.Priority)
			}
		} else {
			pq.rejected.Add(1)
			return fmt.Errorf("pqueue[%s]: full (%d items)", pq.cfg.Name, pq.h.Len())
		}
	}

	entry := &PQItem[T]{
		Value:      item,
		Priority:   priority,
		EnqueuedAt: time.Now(),
	}
	heap.Push(&pq.h, entry)
	pq.enqueued.Add(1)
	pq.cond.Signal()

	if pq.cfg.OnEnqueue != nil {
		pq.cfg.OnEnqueue(pq.cfg.Name, pq.h.Len(), priority)
	}
	return nil
}

// Dequeue removes and returns the highest-priority item.
// Blocks until an item is available or ctx is cancelled.
func (pq *PriorityQueue[T]) Dequeue(ctx context.Context) (T, int, error) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for pq.h.Len() == 0 {
		// Wait for signal or context cancellation
		waitDone := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				pq.cond.Broadcast()
			case <-waitDone:
			}
		}()
		pq.cond.Wait()
		close(waitDone)

		if pq.h.Len() == 0 {
			select {
			case <-ctx.Done():
				var zero T
				return zero, 0, ctx.Err()
			default:
			}
		}
	}

	item := heap.Pop(&pq.h).(*PQItem[T])
	pq.dequeued.Add(1)
	waited := time.Since(item.EnqueuedAt)

	if pq.cfg.OnDequeue != nil {
		pq.cfg.OnDequeue(pq.cfg.Name, pq.h.Len(), item.Priority, waited)
	}
	return item.Value, item.Priority, nil
}

// TryDequeue non-blocking dequeue. Returns (zero, 0, false) if empty.
func (pq *PriorityQueue[T]) TryDequeue() (T, int, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if pq.h.Len() == 0 {
		var zero T
		return zero, 0, false
	}
	item := heap.Pop(&pq.h).(*PQItem[T])
	pq.dequeued.Add(1)
	return item.Value, item.Priority, true
}

// Peek returns the highest-priority item without removing it.
// Returns (zero, 0, false) if empty.
func (pq *PriorityQueue[T]) Peek() (T, int, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if pq.h.Len() == 0 {
		var zero T
		return zero, 0, false
	}
	top := pq.h[0]
	return top.Value, top.Priority, true
}

// Len returns the current number of items.
func (pq *PriorityQueue[T]) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.h.Len()
}

// DrainAll removes all items and returns them sorted highest-to-lowest priority.
func (pq *PriorityQueue[T]) DrainAll() []PQItem[T] {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	result := make([]PQItem[T], 0, pq.h.Len())
	for pq.h.Len() > 0 {
		item := heap.Pop(&pq.h).(*PQItem[T])
		result = append(result, *item)
	}
	return result
}

func (pq *PriorityQueue[T]) findLowest() *PQItem[T] {
	if len(pq.h) == 0 {
		return nil
	}
	lowest := pq.h[0]
	for _, item := range pq.h {
		if item.Priority < lowest.Priority {
			lowest = item
		}
	}
	return lowest
}

// Stats returns a snapshot.
func (pq *PriorityQueue[T]) Stats() PriorityQueueStats {
	pq.mu.Lock()
	size := pq.h.Len()
	pq.mu.Unlock()
	return PriorityQueueStats{
		Name:     pq.cfg.Name,
		Size:     int64(size),
		Enqueued: pq.enqueued.Load(),
		Dequeued: pq.dequeued.Load(),
		Evicted:  pq.evicted.Load(),
		Rejected: pq.rejected.Load(),
	}
}

// PriorityQueueStats is a point-in-time snapshot.
type PriorityQueueStats struct {
	Name     string
	Size     int64
	Enqueued int64
	Dequeued int64
	Evicted  int64
	Rejected int64
}
