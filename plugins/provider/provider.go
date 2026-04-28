package provider

import "context"

type Provider interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

func StartAll(ctx context.Context, providers []Provider) error {
	for _, p := range providers {
		if err := p.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func StopAll(ctx context.Context, providers []Provider) error {
	for i := len(providers) - 1; i >= 0; i-- {
		if err := providers[i].Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}
