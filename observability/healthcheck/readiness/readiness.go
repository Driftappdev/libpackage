package healthcheck

import "context"

type Check func(context.Context) error

type Readiness struct{ checks []Check }

func NewReadiness(checks ...Check) *Readiness { return &Readiness{checks: checks} }

func (r *Readiness) Ready(ctx context.Context) error {
	for _, check := range r.checks {
		if err := check(ctx); err != nil {
			return err
		}
	}
	return nil
}
