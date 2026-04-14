package shutdown

import "context"

type NamedHook struct {
	Name string
	Fn   Hook
}

func (h NamedHook) Run(ctx context.Context) error {
	if h.Fn == nil {
		return nil
	}
	return h.Fn(ctx)
}
