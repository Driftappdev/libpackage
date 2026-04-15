package jwt

import (
	"crypto/rsa"
	"errors"
	"time"

	gjwt "github.com/golang-jwt/jwt/v5"
)

type AdvancedClaims struct {
	Subject   string            `json:"sub,omitempty"`
	Issuer    string            `json:"iss,omitempty"`
	Audience  []string          `json:"aud,omitempty"`
	ExpiresAt time.Time         `json:"exp,omitempty"`
	IssuedAt  time.Time         `json:"iat,omitempty"`
	NotBefore time.Time         `json:"nbf,omitempty"`
	ID        string            `json:"jti,omitempty"`
	Scope     []string          `json:"scope,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

func (c AdvancedClaims) registered() gjwt.RegisteredClaims {
	var aud gjwt.ClaimStrings
	if len(c.Audience) > 0 {
		aud = append(aud, c.Audience...)
	}
	rc := gjwt.RegisteredClaims{
		Subject:  c.Subject,
		Issuer:   c.Issuer,
		Audience: aud,
		ID:       c.ID,
	}
	if !c.ExpiresAt.IsZero() {
		rc.ExpiresAt = gjwt.NewNumericDate(c.ExpiresAt)
	}
	if !c.IssuedAt.IsZero() {
		rc.IssuedAt = gjwt.NewNumericDate(c.IssuedAt)
	}
	if !c.NotBefore.IsZero() {
		rc.NotBefore = gjwt.NewNumericDate(c.NotBefore)
	}
	return rc
}

type mapClaims struct {
	gjwt.RegisteredClaims
	Scope []string          `json:"scope,omitempty"`
	Extra map[string]string `json:"extra,omitempty"`
}

type Signer struct {
	HMACSecret []byte
	PrivateKey *rsa.PrivateKey
	Method     gjwt.SigningMethod
}

type Verifier struct {
	HMACSecret []byte
	PublicKey  *rsa.PublicKey
	Methods    []string
	Issuer     string
	Audience   string
	ClockSkew  time.Duration
}

func (s Signer) SignAdvanced(claims AdvancedClaims) (string, error) {
	if s.Method == nil {
		s.Method = gjwt.SigningMethodHS256
	}
	token := gjwt.NewWithClaims(s.Method, mapClaims{
		RegisteredClaims: claims.registered(),
		Scope:            claims.Scope,
		Extra:            claims.Extra,
	})
	switch s.Method {
	case gjwt.SigningMethodHS256, gjwt.SigningMethodHS384, gjwt.SigningMethodHS512:
		return token.SignedString(s.HMACSecret)
	case gjwt.SigningMethodRS256, gjwt.SigningMethodRS384, gjwt.SigningMethodRS512:
		if s.PrivateKey == nil {
			return "", errors.New("jwt: private key is required")
		}
		return token.SignedString(s.PrivateKey)
	default:
		return "", errors.New("jwt: unsupported signing method")
	}
}

func (v Verifier) VerifyAdvanced(raw string) (*AdvancedClaims, error) {
	parser := gjwt.NewParser(gjwt.WithValidMethods(v.Methods), gjwt.WithLeeway(v.ClockSkew))
	tok, err := parser.ParseWithClaims(raw, &mapClaims{}, func(token *gjwt.Token) (any, error) {
		switch token.Method {
		case gjwt.SigningMethodHS256, gjwt.SigningMethodHS384, gjwt.SigningMethodHS512:
			return v.HMACSecret, nil
		case gjwt.SigningMethodRS256, gjwt.SigningMethodRS384, gjwt.SigningMethodRS512:
			if v.PublicKey == nil {
				return nil, errors.New("jwt: public key is required")
			}
			return v.PublicKey, nil
		default:
			return nil, errors.New("jwt: unsupported signing method")
		}
	})
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*mapClaims)
	if !ok || !tok.Valid {
		return nil, errors.New("jwt: invalid token")
	}
	if v.Issuer != "" && claims.Issuer != v.Issuer {
		return nil, errors.New("jwt: invalid issuer")
	}
	if v.Audience != "" {
		found := false
		for _, aud := range claims.Audience {
			if aud == v.Audience {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("jwt: invalid audience")
		}
	}
	out := &AdvancedClaims{
		Subject: claims.Subject,
		Issuer:  claims.Issuer,
		ID:      claims.ID,
		Scope:   claims.Scope,
		Extra:   claims.Extra,
	}
	if claims.ExpiresAt != nil {
		out.ExpiresAt = claims.ExpiresAt.Time
	}
	if claims.IssuedAt != nil {
		out.IssuedAt = claims.IssuedAt.Time
	}
	if claims.NotBefore != nil {
		out.NotBefore = claims.NotBefore.Time
	}
	if len(claims.Audience) > 0 {
		out.Audience = append([]string(nil), claims.Audience...)
	}
	return out, nil
}
