package healthcheck

import "context"

type Liveness struct{ alive func(context.Context) error }

func NewLiveness(fn func(context.Context) error) *Liveness { return &Liveness{alive: fn} }
func (l *Liveness) Live(ctx context.Context) error {
	if l == nil || l.alive == nil {
		return nil
	}
	return l.alive(ctx)
}
