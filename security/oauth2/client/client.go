package oauth2

import (
	"context"
	"errors"
	"net/url"

	xoauth2 "golang.org/x/oauth2"
)

type Client struct {
	Config xoauth2.Config
}

func (c Client) AuthCodeURL(state string, opts ...xoauth2.AuthCodeOption) string {
	return c.Config.AuthCodeURL(state, opts...)
}

func (c Client) Exchange(ctx context.Context, code string, opts ...xoauth2.AuthCodeOption) (*xoauth2.Token, error) {
	return c.Config.Exchange(ctx, code, opts...)
}

func (c Client) TokenSource(ctx context.Context, token *xoauth2.Token) xoauth2.TokenSource {
	return c.Config.TokenSource(ctx, token)
}

type ProviderMetadata struct {
	Issuer                string   `json:"issuer" yaml:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint" yaml:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint" yaml:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint,omitempty" yaml:"userinfo_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty" yaml:"scopes_supported,omitempty"`
}

func ValidateMetadata(m ProviderMetadata) error {
	if m.Issuer == "" {
		return errors.New("oauth2: issuer is required")
	}
	for _, raw := range []string{m.AuthorizationEndpoint, m.TokenEndpoint} {
		if raw == "" {
			return errors.New("oauth2: endpoints are required")
		}
		if _, err := url.ParseRequestURI(raw); err != nil {
			return err
		}
	}
	return nil
}
