package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

func Do(ctx context.Context, policy Policy, fn func(context.Context) error) error {
	if policy.MaxAttempts <= 0 {
		policy = DefaultPolicy()
	}
	var lastErr error
	delay := policy.BaseDelay
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := fn(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == policy.MaxAttempts {
			break
		}
		sleep := delay
		if policy.Jitter && sleep > 0 {
			sleep = time.Duration(rand.Int63n(int64(sleep)))
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), lastErr)
		case <-timer.C:
		}
		next := time.Duration(float64(delay) * policy.Multiplier)
		if next <= 0 || (policy.MaxDelay > 0 && next > policy.MaxDelay) {
			next = policy.MaxDelay
		}
		delay = next
	}
	return lastErr
}
