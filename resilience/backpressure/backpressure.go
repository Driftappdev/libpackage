package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BackpressureStrategy determines how overflow is handled.
type BackpressureStrategy int

const (
	// BackpressureDrop silently drops new work when the system is at capacity.
	BackpressureDrop BackpressureStrategy = iota
	// BackpressureBlock blocks the caller until capacity is available.
	BackpressureBlock
	// BackpressureDropOldest evicts the oldest queued item to make room.
	BackpressureDropOldest
	// BackpressureError returns an error to the caller immediately.
	BackpressureError
)

// BackpressureConfig configures a backpressure controller.
type BackpressureConfig struct {
	// Name of this controller (for metrics/logging).
	Name string
	// HighWatermark is the queue length at which backpressure activates.
	HighWatermark int
	// LowWatermark is the queue length at which backpressure deactivates.
	LowWatermark int
	// Strategy determines the overflow behavior.
	Strategy BackpressureStrategy
	// BlockTimeout is the max time to block (for BackpressureBlock strategy).
	BlockTimeout time.Duration
	// OnPressureOn is called when pressure activates.
	OnPressureOn func(name string, queueLen int)
	// OnPressureOff is called when pressure deactivates.
	OnPressureOff func(name string, queueLen int)
	// OnDrop is called when an item is dropped.
	OnDrop func(name string, reason string)
}

// BackpressureController monitors queue depth and enforces flow control.
// It is designed to wrap any channel-based producer and protect consumers.
type BackpressureController[T any] struct {
	cfg         BackpressureConfig
	queue       []T
	mu          sync.Mutex
	pressureOn  bool
	cond        *sync.Cond
	closed      bool

	// metrics
	submitted atomic.Int64
	dropped   atomic.Int64
	accepted  atomic.Int64
	pressures atomic.Int64 // times pressure turned on
}

// NewBackpressureController creates a controller.
func NewBackpressureController[T any](cfg BackpressureConfig) *BackpressureController[T] {
	if cfg.Name == "" {
		cfg.Name = "default"
	}
	if cfg.LowWatermark <= 0 {
		cfg.LowWatermark = cfg.HighWatermark / 2
	}
	bc := &BackpressureController[T]{cfg: cfg}
	bc.cond = sync.NewCond(&bc.mu)
	return bc
}

// Offer submits an item subject to backpressure policy.
// Returns nil on acceptance, error on drop/rejection.
func (bc *BackpressureController[T]) Offer(ctx context.Context, item T) error {
	bc.submitted.Add(1)

	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.closed {
		return fmt.Errorf("backpressure[%s]: controller is closed", bc.cfg.Name)
	}

	qlen := len(bc.queue)

	// Check high watermark
	if qlen >= bc.cfg.HighWatermark {
		if !bc.pressureOn {
			bc.pressureOn = true
			bc.pressures.Add(1)
			if bc.cfg.OnPressureOn != nil {
				go bc.cfg.OnPressureOn(bc.cfg.Name, qlen)
			}
		}

		switch bc.cfg.Strategy {
		case BackpressureDrop:
			bc.dropped.Add(1)
			if bc.cfg.OnDrop != nil {
				go bc.cfg.OnDrop(bc.cfg.Name, "high watermark, drop new")
			}
			return fmt.Errorf("backpressure[%s]: dropped (high watermark %d)", bc.cfg.Name, qlen)

		case BackpressureError:
			bc.dropped.Add(1)
			return fmt.Errorf("backpressure[%s]: backpressure active (%d/%d)",
				bc.cfg.Name, qlen, bc.cfg.HighWatermark)

		case BackpressureDropOldest:
			if len(bc.queue) > 0 {
				bc.queue = bc.queue[1:] // evict oldest
				bc.dropped.Add(1)
				if bc.cfg.OnDrop != nil {
					go bc.cfg.OnDrop(bc.cfg.Name, "evicted oldest")
				}
			}
			// Fall through to enqueue new item

		case BackpressureBlock:
			timeout := bc.cfg.BlockTimeout
			if timeout <= 0 {
				timeout = 5 * time.Second
			}
			deadline := time.Now().Add(timeout)

			for len(bc.queue) >= bc.cfg.HighWatermark {
				remaining := time.Until(deadline)
				if remaining <= 0 {
					bc.dropped.Add(1)
					return fmt.Errorf("backpressure[%s]: block timeout exceeded", bc.cfg.Name)
				}
				// Use a timed wait via goroutine signal
				waitDone := make(chan struct{})
				go func() {
					time.Sleep(remaining)
					bc.cond.Broadcast()
					close(waitDone)
				}()
				bc.cond.Wait()
				select {
				case <-ctx.Done():
					bc.dropped.Add(1)
					return fmt.Errorf("backpressure[%s]: context cancelled while blocking: %w",
						bc.cfg.Name, ctx.Err())
				case <-waitDone:
				default:
				}
			}
		}
	}

	bc.queue = append(bc.queue, item)
	bc.accepted.Add(1)
	bc.cond.Signal()

	// Check low watermark — deactivate pressure
	if bc.pressureOn && len(bc.queue) <= bc.cfg.LowWatermark {
		bc.pressureOn = false
		if bc.cfg.OnPressureOff != nil {
			go bc.cfg.OnPressureOff(bc.cfg.Name, len(bc.queue))
		}
	}
	return nil
}

// Poll removes and returns the next item. Blocks until an item is available
// or the context is cancelled.
func (bc *BackpressureController[T]) Poll(ctx context.Context) (T, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	for len(bc.queue) == 0 && !bc.closed {
		// Wake up periodically to check context
		waitDone := make(chan struct{})
		go func() {
			time.Sleep(50 * time.Millisecond)
			bc.cond.Broadcast()
			close(waitDone)
		}()
		bc.cond.Wait()
		<-waitDone

		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		default:
		}
	}

	if bc.closed && len(bc.queue) == 0 {
		var zero T
		return zero, fmt.Errorf("backpressure[%s]: closed", bc.cfg.Name)
	}

	item := bc.queue[0]
	bc.queue = bc.queue[1:]
	bc.cond.Signal()

	// Deactivate pressure after drain
	if bc.pressureOn && len(bc.queue) <= bc.cfg.LowWatermark {
		bc.pressureOn = false
		if bc.cfg.OnPressureOff != nil {
			go bc.cfg.OnPressureOff(bc.cfg.Name, len(bc.queue))
		}
	}
	return item, nil
}

// TryPoll non-blocking poll. Returns (zero, false) if queue is empty.
func (bc *BackpressureController[T]) TryPoll() (T, bool) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.queue) == 0 {
		var zero T
		return zero, false
	}
	item := bc.queue[0]
	bc.queue = bc.queue[1:]
	bc.cond.Signal()
	return item, true
}

// Close shuts down the controller, unblocking all waiters.
func (bc *BackpressureController[T]) Close() {
	bc.mu.Lock()
	bc.closed = true
	bc.cond.Broadcast()
	bc.mu.Unlock()
}

// IsPressureActive returns true if the high watermark is currently exceeded.
func (bc *BackpressureController[T]) IsPressureActive() bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.pressureOn
}

// BackpressureStats is a point-in-time snapshot.
type BackpressureStats struct {
	Name        string
	QueueLen    int
	Submitted   int64
	Accepted    int64
	Dropped     int64
	Pressures   int64
	PressureOn  bool
}

// Stats returns a snapshot.
func (bc *BackpressureController[T]) Stats() BackpressureStats {
	bc.mu.Lock()
	ql := len(bc.queue)
	pon := bc.pressureOn
	bc.mu.Unlock()
	return BackpressureStats{
		Name:       bc.cfg.Name,
		QueueLen:   ql,
		Submitted:  bc.submitted.Load(),
		Accepted:   bc.accepted.Load(),
		Dropped:    bc.dropped.Load(),
		Pressures:  bc.pressures.Load(),
		PressureOn: pon,
	}
}
