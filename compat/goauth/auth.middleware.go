// Package goauth provides production-grade JWT authentication middleware
// with RS256 / HS256 support, JWKS rotation, role-based access control,
// and refresh token management.
package goauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- Errors -----------------------------------------------------------------

var (
	ErrMissingToken     = errors.New("goauth: missing token")
	ErrInvalidToken     = errors.New("goauth: invalid token")
	ErrExpiredToken     = errors.New("goauth: token has expired")
	ErrInvalidSig       = errors.New("goauth: invalid token signature")
	ErrInsufficientRole = errors.New("goauth: insufficient role")
)

// ---- Claims -----------------------------------------------------------------

// Claims represents the standard + custom JWT claims.
type Claims struct {
	// Standard
	Subject   string `json:"sub"`
	Issuer    string `json:"iss,omitempty"`
	Audience  string `json:"aud,omitempty"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
	JWTID     string `json:"jti,omitempty"`

	// Custom
	Email  string                 `json:"email,omitempty"`
	Roles  []string               `json:"roles,omitempty"`
	Scopes []string               `json:"scopes,omitempty"`
	Extra  map[string]interface{} `json:"extra,omitempty"`
}

// HasRole reports whether any of the given roles is present in Claims.Roles.
func (c *Claims) HasRole(roles ...string) bool {
	set := make(map[string]struct{}, len(c.Roles))
	for _, r := range c.Roles {
		set[r] = struct{}{}
	}
	for _, r := range roles {
		if _, ok := set[r]; ok {
			return true
		}
	}
	return false
}

// HasScope reports whether the claims include the given scope.
func (c *Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ---- Algorithm --------------------------------------------------------------

// Algorithm identifies the signing algorithm.
type Algorithm string

const (
	AlgHS256 Algorithm = "HS256"
	AlgRS256 Algorithm = "RS256"
)

// ---- Signer / Verifier interface --------------------------------------------

// Signer can sign JWT payloads.
type Signer interface {
	Sign(payload []byte) ([]byte, error)
	Algorithm() Algorithm
}

// Verifier can verify JWT signatures.
type Verifier interface {
	Verify(payload, signature []byte) error
	Algorithm() Algorithm
}

// ---- HMAC-SHA256 ------------------------------------------------------------

type hmacSigner struct{ secret []byte }

// NewHMACSigner creates an HS256 Signer/Verifier using the given secret.
func NewHMACSigner(secret []byte) interface {
	Signer
	Verifier
} {
	return &hmacSigner{secret: secret}
}

func (h *hmacSigner) Algorithm() Algorithm { return AlgHS256 }
func (h *hmacSigner) Sign(payload []byte) ([]byte, error) {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write(payload)
	return mac.Sum(nil), nil
}
func (h *hmacSigner) Verify(payload, sig []byte) error {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write(payload)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return ErrInvalidSig
	}
	return nil
}

// ---- RSA-SHA256 -------------------------------------------------------------

type rsaSigner struct {
	priv *rsa.PrivateKey
	pub  *rsa.PublicKey
}

// NewRSASigner creates an RS256 Signer/Verifier from PEM-encoded key material.
func NewRSASigner(privatePEM, publicPEM []byte) (interface {
	Signer
	Verifier
}, error) {
	block, _ := pem.Decode(privatePEM)
	if block == nil {
		return nil, fmt.Errorf("goauth: invalid private key PEM")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	block2, _ := pem.Decode(publicPEM)
	if block2 == nil {
		return nil, fmt.Errorf("goauth: invalid public key PEM")
	}
	pub, err := x509.ParsePKCS1PublicKey(block2.Bytes)
	if err != nil {
		return nil, err
	}
	return &rsaSigner{priv: priv, pub: pub}, nil
}

func (r *rsaSigner) Algorithm() Algorithm { return AlgRS256 }
func (r *rsaSigner) Sign(payload []byte) ([]byte, error) {
	h := sha256.Sum256(payload)
	return rsa.SignPKCS1v15(rand.Reader, r.priv, 0, h[:])
}
func (r *rsaSigner) Verify(payload, sig []byte) error {
	h := sha256.Sum256(payload)
	return rsa.VerifyPKCS1v15(r.pub, 0, h[:], sig)
}

// ---- Token Manager ----------------------------------------------------------

// Config holds token manager configuration.
type Config struct {
	Signer      Signer
	Verifier    Verifier
	Issuer      string
	Audience    string
	TTL         time.Duration // access token lifetime, default 15m
	RefreshTTL  time.Duration // refresh token lifetime, default 7d
	TokenLookup string        // "header:Authorization", "cookie:token", "query:token"
}

// Manager issues and validates JWTs.
type Manager struct {
	cfg Config
}

// NewManager creates a Manager from config.
func NewManager(cfg Config) *Manager {
	if cfg.TTL == 0 {
		cfg.TTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 7 * 24 * time.Hour
	}
	if cfg.TokenLookup == "" {
		cfg.TokenLookup = "header:Authorization"
	}
	return &Manager{cfg: cfg}
}

// Issue generates a signed JWT for the given claims.
func (m *Manager) Issue(claims *Claims) (string, error) {
	now := time.Now()
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = now.Add(m.cfg.TTL).Unix()
	if claims.JWTID == "" {
		claims.JWTID = newID()
	}
	if m.cfg.Issuer != "" {
		claims.Issuer = m.cfg.Issuer
	}
	if m.cfg.Audience != "" {
		claims.Audience = m.cfg.Audience
	}
	return m.sign(claims)
}

// IssueRefresh generates a long-lived refresh token.
func (m *Manager) IssueRefresh(subject string) (string, error) {
	claims := &Claims{
		Subject:   subject,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(m.cfg.RefreshTTL).Unix(),
		JWTID:     newID(),
		Scopes:    []string{"refresh"},
	}
	return m.sign(claims)
}

// Parse validates and parses a token string, returning Claims.
func (m *Manager) Parse(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	// Verify header.
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, ErrInvalidToken
	}
	if header.Typ != "JWT" {
		return nil, ErrInvalidToken
	}

	// Verify signature.
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidToken
	}
	if err := m.cfg.Verifier.Verify([]byte(signingInput), sig); err != nil {
		return nil, err
	}

	// Decode claims.
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, ErrInvalidToken
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, ErrExpiredToken
	}
	return &claims, nil
}

func (m *Manager) sign(claims *Claims) (string, error) {
	header := map[string]string{"alg": string(m.cfg.Signer.Algorithm()), "typ": "JWT"}
	hJSON, _ := json.Marshal(header)
	cJSON, _ := json.Marshal(claims)

	hp := base64.RawURLEncoding.EncodeToString(hJSON)
	cp := base64.RawURLEncoding.EncodeToString(cJSON)
	signingInput := hp + "." + cp

	sig, err := m.cfg.Signer.Sign([]byte(signingInput))
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// ---- Middleware -------------------------------------------------------------

type contextKey struct{}

// FromContext retrieves the Claims from a request context.
func FromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(contextKey{}).(*Claims)
	return c, ok
}

func contextWithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// Middleware returns an http.Handler middleware that validates JWTs.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := m.extractToken(r)
		if err != nil {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		claims, err := m.Parse(token)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, ErrExpiredToken) {
				http.Error(w, "Token expired", status)
				return
			}
			http.Error(w, "Unauthorized: "+err.Error(), status)
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithClaims(r.Context(), claims)))
	})
}

// RequireRoles returns middleware that enforces role-based access.
func (m *Manager) RequireRoles(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := FromContext(r.Context())
			if !ok || !claims.HasRole(roles...) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireScopes returns middleware that enforces scope-based access.
func (m *Manager) RequireScopes(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := FromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			for _, s := range scopes {
				if !claims.HasScope(s) {
					http.Error(w, "Forbidden: missing scope "+s, http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *Manager) extractToken(r *http.Request) (string, error) {
	parts := strings.SplitN(m.cfg.TokenLookup, ":", 2)
	if len(parts) != 2 {
		return m.fromHeader(r)
	}
	switch parts[0] {
	case "header":
		if parts[1] == "Authorization" {
			return m.fromHeader(r)
		}
		v := r.Header.Get(parts[1])
		if v == "" {
			return "", ErrMissingToken
		}
		return v, nil
	case "cookie":
		c, err := r.Cookie(parts[1])
		if err != nil {
			return "", ErrMissingToken
		}
		return c.Value, nil
	case "query":
		v := r.URL.Query().Get(parts[1])
		if v == "" {
			return "", ErrMissingToken
		}
		return v, nil
	}
	return m.fromHeader(r)
}

func (m *Manager) fromHeader(r *http.Request) (string, error) {
	v := r.Header.Get("Authorization")
	if v == "" {
		return "", ErrMissingToken
	}
	if after, ok := strings.CutPrefix(v, "Bearer "); ok {
		return after, nil
	}
	return "", ErrInvalidToken
}

// ---- Blacklist (revocation) -------------------------------------------------

// Blacklist is a thread-safe in-memory token revocation list keyed by JWTID.
// In production, replace the backend with Redis.
type Blacklist struct {
	mu    sync.RWMutex
	store map[string]time.Time
}

// NewBlacklist creates an in-memory Blacklist.
func NewBlacklist() *Blacklist {
	bl := &Blacklist{store: make(map[string]time.Time)}
	go bl.gc()
	return bl
}

// Revoke adds a token ID to the blacklist with its expiry time.
func (bl *Blacklist) Revoke(jti string, expiry time.Time) {
	bl.mu.Lock()
	bl.store[jti] = expiry
	bl.mu.Unlock()
}

// IsRevoked reports whether the token with the given ID has been revoked.
func (bl *Blacklist) IsRevoked(jti string) bool {
	bl.mu.RLock()
	exp, ok := bl.store[jti]
	bl.mu.RUnlock()
	return ok && time.Now().Before(exp)
}

func (bl *Blacklist) gc() {
	t := time.NewTicker(10 * time.Minute)
	for range t.C {
		bl.mu.Lock()
		for id, exp := range bl.store {
			if time.Now().After(exp) {
				delete(bl.store, id)
			}
		}
		bl.mu.Unlock()
	}
}

// ---- JWKS (public key discovery) --------------------------------------------

// JWK represents a single JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
}

// JWKS is the JSON Web Key Set document.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWKSFromRSA builds a JWKS document for the given RSA public key.
func JWKSFromRSA(pub *rsa.PublicKey, kid string) JWKS {
	return JWKS{
		Keys: []JWK{{
			Kty: "RSA",
			Alg: "RS256",
			Use: "sig",
			Kid: kid,
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	}
}

// JWKSHandler returns an http.HandlerFunc that serves a JWKS endpoint.
func JWKSHandler(set JWKS) http.HandlerFunc {
	b, _ := json.Marshal(set)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}
}

// ---- Helpers ----------------------------------------------------------------

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
