package oauth2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

func (c ClientCredentials) Valid() bool {
	_, err := url.Parse(c.TokenURL)
	return err == nil && c.ClientID != "" && c.ClientSecret != ""
}

type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// FetchClientCredentialsToken requests access token using client credentials grant.
func FetchClientCredentialsToken(cfg ClientCredentials) (Token, error) {
	if !cfg.Valid() {
		return Token{}, fmt.Errorf("invalid client credentials config")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if len(cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(cfg.Scopes, " "))
	}

	req, err := http.NewRequest(
		http.MethodPost,
		cfg.TokenURL,
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return Token{}, fmt.Errorf("token endpoint status: %d", resp.StatusCode)
	}

	out := Token{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Token{}, err
	}
	return out, nil
}

// ClientCredentialsToken is convenience API compatible with backup implementation.
func ClientCredentialsToken(endpoint, clientID, clientSecret, scope string) (Token, error) {
	cfg := ClientCredentials{
		TokenURL:     endpoint,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}
	if scope != "" {
		cfg.Scopes = []string{scope}
	}
	return FetchClientCredentialsToken(cfg)
}
