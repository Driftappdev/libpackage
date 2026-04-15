package client

import "github.com/driftappdev/libpackage/featureflag/types"

type Provider interface {
	Get(key string) (types.Flag, bool)
}

type Client struct{ provider Provider }

func New(provider Provider) *Client { return &Client{provider: provider} }

func (c *Client) Flag(key string) (types.Flag, bool) {
	if c == nil || c.provider == nil {
		return types.Flag{}, false
	}
	return c.provider.Get(key)
}
