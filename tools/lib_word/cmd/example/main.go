// example/main.go â€” demonstrates wiring all resilience primitives together
// as a unified middleware stack for an enterprise service.
//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/driftappdev/tools/lib_word"
)

func main() {
	ctx := context.Background()

	// â”€â”€ 1. Token Bucket â€” per-IP rate limiting â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	rateLimiter := resilience.NewMultiKeyTokenBucket(resilience.TokenBucketConfig{
		Name:        "api-ratelimit",
		Rate:        100,  // 100 req/s per key
		Burst:       200,
		WaitOnEmpty: false,
		OnThrottle: func(name string, wait time.Duration) {
			log.Printf("[THROTTLE] %s wait=%s", name, wait)
		},
	}, 10_000)

	// â”€â”€ 2. Load Shedder â€” protect against saturation â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	shedder := resilience.NewLoadShedder(resilience.LoadShedderConfig{
		Name:          "api-shedder",
		Strategy:      resilience.ShedLatency,
		LatencyTarget: 200 * time.Millisecond,
		OnShed: func(name, reason string) {
			log.Printf("[SHED] %s reason=%s", name, reason)
		},
	})

	// â”€â”€ 3. Adaptive Concurrency â€” auto-tune parallelism â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	adaptive := resilience.NewAdaptiveConcurrencyLimiter(resilience.AdaptiveConcurrencyConfig{
		Name:         "db-adaptive",
		Algorithm:    resilience.AlgoGradient,
		InitialLimit: 50,
		MinLimit:     5,
		MaxLimit:     200,
		SampleWindow: 500 * time.Millisecond,
		TargetRTT:    20 * time.Millisecond,
		OnLimitChange: func(name string, old, new int) {
			log.Printf("[ADAPTIVE] %s limit %dâ†’%d", name, old, new)
		},
	})
	defer adaptive.Close()

	// â”€â”€ 4. Bulkhead â€” isolate DB pool â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	dbBulkhead := resilience.NewBulkhead(resilience.BulkheadConfig{
		Name:           "db-pool",
		MaxConcurrent:  20,
		MaxQueue:       100,
		AcquireTimeout: 500 * time.Millisecond,
		OnRejected: func(name, reason string) {
			log.Printf("[BULKHEAD] %s rejected: %s", name, reason)
		},
	})

	// â”€â”€ 5. Limiter â€” hard concurrency cap â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	limiter := resilience.NewLimiter(resilience.LimiterConfig{
		MaxConcurrent:  30,
		QueueSize:      500,
		AcquireTimeout: 1 * time.Second,
	})

	// â”€â”€ 6. Retry + Backoff â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	retryer := resilience.ExponentialRetryer(5, 50*time.Millisecond, 5*time.Second)

	// â”€â”€ 7. Deadline Enforcer â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	deadlineEnforcer := resilience.NewDeadlineEnforcer(resilience.DeadlineConfig{
		Name:    "api-deadline",
		Hard:    2 * time.Second,
		Soft:    1500 * time.Millisecond,
		OnSoftBreach: func(name string, elapsed, budget time.Duration) {
			log.Printf("[SOFT DEADLINE] %s elapsed=%s remaining=%s", name, elapsed, budget)
		},
		OnHardBreach: func(name string, elapsed time.Duration) {
			log.Printf("[HARD DEADLINE EXCEEDED] %s after %s", name, elapsed)
		},
	})

	// â”€â”€ 8. Priority Queue â€” prioritize premium users â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	pq := resilience.NewPriorityQueue[string](resilience.PriorityQueueConfig{
		Name:        "request-pq",
		MaxSize:     1000,
		EvictLowest: true,
	})

	// â”€â”€ 9. Batcher â€” batch DB writes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	batcher := resilience.NewBatcher(resilience.BatcherConfig[string]{
		Name:    "db-writes",
		MaxSize: 100,
		MaxWait: 20 * time.Millisecond,
		FlushFn: func(ctx context.Context, batch []string) error {
			log.Printf("[BATCH] flushing %d items", len(batch))
			return nil
		},
		OnFlush: func(name string, count int, duration time.Duration, err error) {
			log.Printf("[BATCH] %s: %d items in %s err=%v", name, count, duration, err)
		},
	})
	defer batcher.Close()

	// â”€â”€ 10. Backpressure â€” protect write pipeline â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	bp := resilience.NewBackpressureController[string](resilience.BackpressureConfig{
		Name:           "write-pipeline",
		HighWatermark:  500,
		LowWatermark:   100,
		Strategy:       resilience.BackpressureDropOldest,
		OnPressureOn: func(name string, queueLen int) {
			log.Printf("[BACKPRESSURE ON] %s qlen=%d", name, queueLen)
		},
		OnPressureOff: func(name string, queueLen int) {
			log.Printf("[BACKPRESSURE OFF] %s qlen=%d", name, queueLen)
		},
	})
	defer bp.Close()

	// â”€â”€ 11. WorkQueue â€” background task processing â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	wq := resilience.NewWorkQueue(resilience.WorkQueueConfig{
		Workers:    8,
		QueueDepth: 256,
		PanicHandler: func(workerID int, v interface{}) {
			log.Printf("[PANIC] worker %d: %v", workerID, v)
		},
	}, func(ctx context.Context, item resilience.WorkItem[string]) (any, error) {
		time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
		return fmt.Sprintf("processed: %s", item.Payload), nil
	})
	defer wq.Close()

	// â”€â”€ 12. Checkpoint â€” track processing progress â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	store, err := resilience.NewFileCheckpointStore[int64, map[string]int]("/tmp/checkpoints")
	if err != nil {
		log.Fatal(err)
	}
	cp := resilience.NewCheckpointManager(resilience.CheckpointConfig{
		ID:               "main-processor",
		AutoSaveInterval: 30 * time.Second,
		SaveThreshold:    1000,
		OnSave: func(id string, version int64) {
			log.Printf("[CHECKPOINT] saved %s v%d", id, version)
		},
	}, store)
	defer cp.Close()

	// â”€â”€ 13. Health Supervisor â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	supervisor := resilience.NewHealthSupervisor(resilience.HealthSupervisorConfig{
		Name:                 "main-service",
		CheckInterval:        15 * time.Second,
		CheckTimeout:         3 * time.Second,
		ConsecutiveFailures:  3,
		ConsecutiveSuccesses: 2,
		DegradedThreshold:   0.2,
		UnhealthyThreshold:  0.5,
		OnStatusChange: func(name string, old, new resilience.HealthStatus) {
			log.Printf("[HEALTH] %s: %s â†’ %s", name, old, new)
		},
	})
	defer supervisor.Close()

	supervisor.RegisterFunc("database", func(ctx context.Context) error {
		// simulate DB ping
		return nil
	}, true /* critical */)
	supervisor.RegisterFunc("cache", func(ctx context.Context) error {
		return nil
	}, false)

	// â”€â”€ Full request pipeline â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	handleRequest := func(userID string, priority int, payload string) error {
		// Step 1: Rate limit
		if !rateLimiter.Allow(userID, 1) {
			return fmt.Errorf("rate limited")
		}

		// Step 2: Load shed
		if err := shedder.Allow(ctx, priority); err != nil {
			return err
		}

		// Step 3: Enforce deadline across entire chain
		return deadlineEnforcer.Do(ctx, func(ctx context.Context) error {
			// Step 4: Adaptive concurrency slot
			token, err := adaptive.Acquire(ctx)
			if err != nil {
				return err
			}

			// Step 5: Bulkhead
			bhToken, err := dbBulkhead.Acquire(ctx)
			if err != nil {
				token.Release(false)
				return err
			}

			// Step 6: Hard limiter
			release, err := limiter.Acquire(ctx)
			if err != nil {
				bhToken.Release()
				token.Release(false)
				return err
			}

			defer func() {
				release()
				bhToken.Release()
				token.Release(err == nil)
			}()

			// Step 7: Retry the actual DB operation
			_, err = retryer.Do(ctx, func(ctx context.Context, attempt int) error {
				// Simulate DB write via batcher
				return batcher.Add(ctx, payload)
			})
			if err != nil {
				return err
			}

			// Step 8: Checkpoint progress
			return cp.Commit(int64(time.Now().UnixNano()), map[string]int{"count": 1}, nil)
		})
	}

	// â”€â”€ Enqueue priority requests â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	for i := 0; i < 10; i++ {
		priority := rand.Intn(10)
		_ = pq.Enqueue(fmt.Sprintf("req-%d", i), priority)
	}

	// â”€â”€ Process from priority queue â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	for {
		val, _, ok := pq.TryDequeue()
		if !ok {
			break
		}
		if err := handleRequest("user-1", 5, val); err != nil {
			log.Printf("[ERR] %v", err)
		} else {
			log.Printf("[OK] processed %s", val)
		}
	}

	// â”€â”€ Print all stats â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	fmt.Printf("\n=== STATS ===\n")
	fmt.Printf("Limiter:   %+v\n", limiter.Stats())
	fmt.Printf("Bulkhead:  %+v\n", dbBulkhead.Stats())
	fmt.Printf("Adaptive:  %+v\n", adaptive.Stats())
	fmt.Printf("Shedder:   %+v\n", shedder.Stats())
	fmt.Printf("PQueue:    %+v\n", pq.Stats())
	fmt.Printf("Batcher:   %+v\n", batcher.Stats())
	fmt.Printf("WQueue:    %+v\n", wq.Stats())
	fmt.Printf("Checkpoint:%+v\n", cp.Stats())
	fmt.Printf("Health:    %s\n", supervisor.Report().Summary())
	fmt.Printf("RateKeys:  %d\n", rateLimiter.KeyCount())
}


