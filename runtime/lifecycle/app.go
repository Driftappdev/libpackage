package lifecycle

import (
	"context"
	"errors"
)

type App struct{ components []Component }

func New(components ...Component) *App { return &App{components: components} }

func (a *App) Start(ctx context.Context) error {
	for _, c := range a.components {
		if err := c.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	var errs []error
	for i := len(a.components) - 1; i >= 0; i-- {
		if err := a.components[i].Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
