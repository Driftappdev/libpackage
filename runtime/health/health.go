package health

import (
	"context"
	"errors"
)

// MultiChecker aggregates multiple checkers into one.
type MultiChecker struct {
	checkers []Checker
}

func NewMultiChecker(checkers ...Checker) *MultiChecker {
	return &MultiChecker{checkers: checkers}
}

func (m *MultiChecker) Check(ctx context.Context) error {
	var errs []error
	for _, checker := range m.checkers {
		if checker == nil {
			continue
		}
		if err := checker.Check(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
