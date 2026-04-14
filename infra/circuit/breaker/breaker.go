package circuit

import (
	"errors"
	"sync"
	"time"
)

var ErrOpen = errors.New("circuit open")

type Breaker struct {
	mu         sync.Mutex
	failures   int
	threshold  int
	resetAfter time.Duration
	openedAt   time.Time
}

func New(threshold int, resetAfter time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 5
	}
	if resetAfter <= 0 {
		resetAfter = 30 * time.Second
	}
	return &Breaker{threshold: threshold, resetAfter: resetAfter}
}

func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedAt.IsZero() {
		return nil
	}
	if time.Since(b.openedAt) >= b.resetAfter {
		b.failures = 0
		b.openedAt = time.Time{}
		return nil
	}
	return ErrOpen
}

func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openedAt = time.Time{}
}

func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openedAt = time.Now()
	}
}
