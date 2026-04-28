package health

import (
	"context"
	"errors"
	"net/http"
)

type Checker interface{ Check(context.Context) error }

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

func Handler(checker Checker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checker != nil {
			if err := checker.Check(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}
