package goprovider

import "context"

type Provider interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
