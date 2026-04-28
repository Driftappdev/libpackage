// Package clock provides a testable clock abstraction used across obslib.
// Replace with a fake clock in unit tests to control time deterministically.
package clock

import "time"

// Clock abstracts time operations.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// Real is the production system clock.
type Real struct{}

func (Real) Now() time.Time                  { return time.Now() }
func (Real) Since(t time.Time) time.Duration { return time.Since(t) }

// NewReal returns the production clock.
func NewReal() Clock { return Real{} }

// Mock is a controllable clock for testing.
type Mock struct{ current time.Time }

func NewMock(t time.Time) *Mock        { return &Mock{current: t} }
func (m *Mock) Now() time.Time         { return m.current }
func (m *Mock) Since(t time.Time) time.Duration { return m.current.Sub(t) }
func (m *Mock) Advance(d time.Duration) { m.current = m.current.Add(d) }
