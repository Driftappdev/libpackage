package retry

import (
	"context"
	"time"
)

func Do(ctx context.Context, attempts int, backoff func(int) time.Duration, fn func(context.Context) error) error {
	if attempts <= 0 {
		attempts = 1
	}
	if backoff == nil {
		backoff = func(int) time.Duration { return 0 }
	}
	var err error
	for i := 1; i <= attempts; i++ {
		err = fn(ctx)
		if err == nil {
			return nil
		}
		if i == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff(i)):
		}
	}
	return err
}
