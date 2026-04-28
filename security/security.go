// Package security provides enterprise-grade security primitives for Go services.
//
// Features:
//   - JWT token issuance, validation, and rotation
//   - API key hashing and verification (bcrypt + HMAC)
//   - Request signing and verification (HMAC-SHA256)
//   - Rate limiting per identity (token bucket + sliding window)
//   - Brute-force protection with exponential backoff lockout
//   - IP allowlist / denylist
//   - Secrets management interface (Vault, AWS Secrets Manager)
//   - Sensitive data encryption (AES-256-GCM)
//   - CSRF token generation and validation
//   - Security headers middleware
//   - Suspicious activity detection hooks
package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ────────────────────────────────────────────────────────────────────────────────
// Errors
// ────────────────────────────────────────────────────────────────────────────────

var (
	ErrTokenExpired      = errors.New("security: token has expired")
	ErrTokenInvalid      = errors.New("security: token is invalid")
	ErrTokenRevoked      = errors.New("security: token has been revoked")
	ErrRateLimited       = errors.New("security: rate limit exceeded")
	ErrAccountLocked     = errors.New("security: account is temporarily locked")
	ErrIPDenied          = errors.New("security: IP address is denied")
	ErrSignatureMismatch = errors.New("security: request signature mismatch")
	ErrKeyInvalid        = errors.New("security: API key is invalid")
	ErrEncryptionFailed  = errors.New("security: encryption failed")
	ErrDecryptionFailed  = errors.New("security: decryption failed")
)

// ────────────────────────────────────────────────────────────────────────────────
// JWT — lightweight JWT (no external dep, HMAC-SHA256)
// ────────────────────────────────────────────────────────────────────────────────

// Claims is the JWT payload.
type Claims struct {
	// Standard claims.
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud,omitempty"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	JWTID     string `json:"jti"`

	// Custom claims.
	UserID         string   `json:"uid,omitempty"`
	TenantID       string   `json:"tid,omitempty"`
	Roles          []string `json:"roles,omitempty"`
	ServiceName    string   `json:"svc,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	SessionID      string   `json:"sid,omitempty"`
	IdempotencyKey string   `json:"idk,omitempty"`
}

// IsExpired returns true if the token has passed its expiry.
func (c *Claims) IsExpired() bool {
	return time.Now().Unix() > c.ExpiresAt
}

// HasScope returns true if the claims include the given scope.
func (c *Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasRole returns true if the claims include the given role.
func (c *Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// TokenManager handles JWT issuance and validation.
type TokenManager struct {
	signingKey []byte
	issuer     string
	defaultTTL time.Duration
	revoked    sync.Map // jti -> struct{}
}

// NewTokenManager creates a new JWT token manager.
func NewTokenManager(signingKey []byte, issuer string, defaultTTL time.Duration) *TokenManager {
	if defaultTTL == 0 {
		defaultTTL = time.Hour
	}
	return &TokenManager{signingKey: signingKey, issuer: issuer, defaultTTL: defaultTTL}
}

// Issue signs and returns a new JWT token string.
func (tm *TokenManager) Issue(claims *Claims) (string, error) {
	if claims.IssuedAt == 0 {
		claims.IssuedAt = time.Now().Unix()
	}
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = time.Now().Add(tm.defaultTTL).Unix()
	}
	if claims.Issuer == "" {
		claims.Issuer = tm.issuer
	}
	if claims.JWTID == "" {
		claims.JWTID = randomHex(16)
	}

	header := base64URLEncode(mustJSON(map[string]string{"alg": "HS256", "typ": "JWT"}))
	payload := base64URLEncode(mustJSON(claims))
	sigInput := header + "." + payload
	sig := tm.sign([]byte(sigInput))
	return sigInput + "." + base64URLEncode(sig), nil
}

// Validate parses and validates a JWT token string.
func (tm *TokenManager) Validate(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrTokenInvalid
	}

	sigInput := parts[0] + "." + parts[1]
	sigBytes, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, ErrTokenInvalid
	}

	expected := tm.sign([]byte(sigInput))
	if !hmac.Equal(sigBytes, expected) {
		return nil, ErrTokenInvalid
	}

	payloadBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, ErrTokenInvalid
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, ErrTokenInvalid
	}

	if claims.IsExpired() {
		return nil, ErrTokenExpired
	}

	if _, revoked := tm.revoked.Load(claims.JWTID); revoked {
		return nil, ErrTokenRevoked
	}

	return &claims, nil
}

// Revoke adds a token JTI to the revocation list.
func (tm *TokenManager) Revoke(jti string) {
	tm.revoked.Store(jti, struct{}{})
}

// Refresh issues a new token from valid existing claims.
func (tm *TokenManager) Refresh(token string) (string, error) {
	claims, err := tm.Validate(token)
	if err != nil {
		return "", err
	}
	claims.IssuedAt = time.Now().Unix()
	claims.ExpiresAt = time.Now().Add(tm.defaultTTL).Unix()
	claims.JWTID = randomHex(16)
	return tm.Issue(claims)
}

func (tm *TokenManager) sign(data []byte) []byte {
	h := hmac.New(sha256.New, tm.signingKey)
	h.Write(data)
	return h.Sum(nil)
}

// ────────────────────────────────────────────────────────────────────────────────
// API Key Management
// ────────────────────────────────────────────────────────────────────────────────

// APIKey represents a hashed API key.
type APIKey struct {
	ID          string            `json:"id"`
	Hash        string            `json:"hash"`   // bcrypt hash of the raw key
	Prefix      string            `json:"prefix"` // First 8 chars (for lookup)
	Name        string            `json:"name"`
	ServiceName string            `json:"service_name"`
	TenantID    string            `json:"tenant_id,omitempty"`
	Scopes      []string          `json:"scopes"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time        `json:"last_used_at,omitempty"`
	Revoked     bool              `json:"revoked"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// APIKeyManager handles API key generation and verification.
type APIKeyManager struct {
	hmacKey    []byte
	bcryptCost int
}

// NewAPIKeyManager creates a new API key manager.
func NewAPIKeyManager(hmacKey []byte) *APIKeyManager {
	return &APIKeyManager{hmacKey: hmacKey, bcryptCost: bcrypt.DefaultCost}
}

// Generate creates a new raw API key and its hashed record.
func (m *APIKeyManager) Generate(name, serviceName, tenantID string, scopes []string, ttl *time.Duration) (rawKey string, record *APIKey, err error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", nil, fmt.Errorf("security: failed to generate key: %w", err)
	}

	// Format: "sk_" + service prefix + hex.
	prefix := "sk_" + sanitizeServiceName(serviceName) + "_"
	rawKey = prefix + hex.EncodeToString(rawBytes)
	keyPrefix := rawKey[:min(len(rawKey), 12)]

	// Hash with bcrypt.
	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), m.bcryptCost)
	if err != nil {
		return "", nil, fmt.Errorf("security: bcrypt failed: %w", err)
	}

	// HMAC for fast lookup (secondary verification).
	hmacHash := m.hmacHash(rawKey)

	record = &APIKey{
		ID:          randomHex(16),
		Hash:        string(hash),
		Prefix:      keyPrefix + ":" + hmacHash[:8],
		Name:        name,
		ServiceName: serviceName,
		TenantID:    tenantID,
		Scopes:      scopes,
		CreatedAt:   time.Now(),
	}

	if ttl != nil {
		exp := time.Now().Add(*ttl)
		record.ExpiresAt = &exp
	}

	return rawKey, record, nil
}

// Verify checks a raw API key against a stored record.
func (m *APIKeyManager) Verify(rawKey string, record *APIKey) error {
	if record.Revoked {
		return ErrKeyInvalid
	}
	if record.ExpiresAt != nil && time.Now().After(*record.ExpiresAt) {
		return ErrTokenExpired
	}

	// Fast HMAC check first.
	hmacHash := m.hmacHash(rawKey)
	storedHMACPart := record.Prefix[strings.LastIndex(record.Prefix, ":")+1:]
	if hmacHash[:8] != storedHMACPart {
		return ErrKeyInvalid
	}

	// Full bcrypt verification.
	if err := bcrypt.CompareHashAndPassword([]byte(record.Hash), []byte(rawKey)); err != nil {
		return ErrKeyInvalid
	}
	return nil
}

func (m *APIKeyManager) hmacHash(key string) string {
	h := hmac.New(sha256.New, m.hmacKey)
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}

// ────────────────────────────────────────────────────────────────────────────────
// Request Signing (HMAC-SHA256 for service-to-service calls)
// ────────────────────────────────────────────────────────────────────────────────

// RequestSigner provides HMAC-SHA256 request signing.
type RequestSigner struct {
	key []byte
}

// NewRequestSigner creates a request signer.
func NewRequestSigner(key []byte) *RequestSigner {
	return &RequestSigner{key: key}
}

// SignedHeaders returns the headers needed to sign a request.
func (s *RequestSigner) SignedHeaders(method, path string, body []byte, timestamp time.Time) map[string]string {
	ts := fmt.Sprintf("%d", timestamp.Unix())
	bodyHash := sha256Hex(body)
	sigInput := strings.Join([]string{method, path, ts, bodyHash}, "\n")

	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(sigInput))
	sig := hex.EncodeToString(h.Sum(nil))

	return map[string]string{
		"X-Signature-Timestamp": ts,
		"X-Signature-Body-Hash": bodyHash,
		"X-Signature":           sig,
	}
}

// Verify verifies an incoming signed request.
func (s *RequestSigner) Verify(method, path string, body []byte, sigTimestamp, sigBodyHash, sig string, maxAge time.Duration) error {
	ts, err := parseUnix(sigTimestamp)
	if err != nil {
		return ErrSignatureMismatch
	}
	if time.Since(ts) > maxAge {
		return ErrSignatureMismatch
	}

	expectedBodyHash := sha256Hex(body)
	if expectedBodyHash != sigBodyHash {
		return ErrSignatureMismatch
	}

	sigInput := strings.Join([]string{method, path, sigTimestamp, sigBodyHash}, "\n")
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(sigInput))
	expected := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return ErrSignatureMismatch
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Rate Limiter (Token Bucket per identity)
// ────────────────────────────────────────────────────────────────────────────────

type bucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	lastTime time.Time
}

func newBucket(capacity float64, ratePerSec float64) *bucket {
	return &bucket{
		tokens:   capacity,
		capacity: capacity,
		rate:     ratePerSec,
		lastTime: time.Now(),
	}
}

func (b *bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()
	b.lastTime = now
	b.tokens = min64(b.capacity, b.tokens+elapsed*b.rate)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimiterConfig configures per-identity rate limits.
type RateLimiterConfig struct {
	Capacity   float64       // Max burst size
	Rate       float64       // Tokens per second
	CleanupTTL time.Duration // How often to clean stale entries
}

// RateLimiter enforces per-identity rate limits.
type RateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*bucket
	config  RateLimiterConfig
}

// NewRateLimiter creates a rate limiter.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	if cfg.CleanupTTL == 0 {
		cfg.CleanupTTL = 10 * time.Minute
	}
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		config:  cfg,
	}
	go rl.cleanup()
	return rl
}

// Allow returns true if the identity is within rate limits.
func (rl *RateLimiter) Allow(identity string) bool {
	rl.mu.RLock()
	b, ok := rl.buckets[identity]
	rl.mu.RUnlock()
	if !ok {
		rl.mu.Lock()
		if b, ok = rl.buckets[identity]; !ok {
			b = newBucket(rl.config.Capacity, rl.config.Rate)
			rl.buckets[identity] = b
		}
		rl.mu.Unlock()
	}
	return b.Allow()
}

// Check returns ErrRateLimited if the identity is over limit.
func (rl *RateLimiter) Check(identity string) error {
	if !rl.Allow(identity) {
		return fmt.Errorf("%w for identity: %s", ErrRateLimited, identity)
	}
	return nil
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.config.CleanupTTL)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.config.CleanupTTL)
		for k, b := range rl.buckets {
			b.mu.Lock()
			if b.lastTime.Before(cutoff) {
				delete(rl.buckets, k)
			}
			b.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// ────────────────────────────────────────────────────────────────────────────────
// Brute-Force Protection (lockout with exponential backoff)
// ────────────────────────────────────────────────────────────────────────────────

type lockoutState struct {
	mu          sync.Mutex
	failures    int
	lockedUntil time.Time
	lastAttempt time.Time
}

// LockoutConfig configures brute-force protection.
type LockoutConfig struct {
	MaxFailures     int
	LockoutDuration time.Duration // Base lockout duration
	MaxLockout      time.Duration // Max lockout cap
	ResetAfter      time.Duration // Reset failure count after this idle time
}

// BruteForceGuard tracks failed attempts and enforces lockouts.
type BruteForceGuard struct {
	mu     sync.RWMutex
	states map[string]*lockoutState
	config LockoutConfig
}

// NewBruteForceGuard creates a brute-force guard.
func NewBruteForceGuard(cfg LockoutConfig) *BruteForceGuard {
	if cfg.MaxFailures == 0 {
		cfg.MaxFailures = 5
	}
	if cfg.LockoutDuration == 0 {
		cfg.LockoutDuration = 5 * time.Minute
	}
	if cfg.MaxLockout == 0 {
		cfg.MaxLockout = 24 * time.Hour
	}
	if cfg.ResetAfter == 0 {
		cfg.ResetAfter = 1 * time.Hour
	}
	return &BruteForceGuard{states: make(map[string]*lockoutState), config: cfg}
}

// Check returns ErrAccountLocked if the identity is locked out.
func (g *BruteForceGuard) Check(identity string) error {
	state := g.getOrCreate(identity)
	state.mu.Lock()
	defer state.mu.Unlock()

	if time.Now().Before(state.lockedUntil) {
		return fmt.Errorf("%w until %s", ErrAccountLocked, state.lockedUntil.Format(time.RFC3339))
	}

	// Reset idle counters.
	if !state.lastAttempt.IsZero() && time.Since(state.lastAttempt) > g.config.ResetAfter {
		state.failures = 0
	}
	return nil
}

// RecordFailure records a failed attempt and potentially triggers lockout.
func (g *BruteForceGuard) RecordFailure(identity string) {
	state := g.getOrCreate(identity)
	state.mu.Lock()
	defer state.mu.Unlock()

	state.failures++
	state.lastAttempt = time.Now()

	if state.failures >= g.config.MaxFailures {
		// Exponential backoff.
		multiplier := time.Duration(1 << uint(state.failures-g.config.MaxFailures))
		lockout := g.config.LockoutDuration * multiplier
		if lockout > g.config.MaxLockout {
			lockout = g.config.MaxLockout
		}
		state.lockedUntil = time.Now().Add(lockout)
	}
}

// RecordSuccess resets the failure count on successful authentication.
func (g *BruteForceGuard) RecordSuccess(identity string) {
	state := g.getOrCreate(identity)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.failures = 0
	state.lockedUntil = time.Time{}
}

func (g *BruteForceGuard) getOrCreate(identity string) *lockoutState {
	g.mu.RLock()
	s, ok := g.states[identity]
	g.mu.RUnlock()
	if !ok {
		g.mu.Lock()
		if s, ok = g.states[identity]; !ok {
			s = &lockoutState{}
			g.states[identity] = s
		}
		g.mu.Unlock()
	}
	return s
}

// ────────────────────────────────────────────────────────────────────────────────
// IP Filter (allowlist + denylist)
// ────────────────────────────────────────────────────────────────────────────────

// IPFilter enforces IP allowlist and denylist.
type IPFilter struct {
	mu        sync.RWMutex
	allowlist []*net.IPNet
	denylist  []*net.IPNet
	mode      string // "allowlist" or "denylist"
}

// NewIPFilter creates an IP filter. mode: "allowlist" or "denylist".
func NewIPFilter(mode string) *IPFilter {
	return &IPFilter{mode: mode}
}

// AddAllow adds a CIDR range to the allowlist.
func (f *IPFilter) AddAllow(cidr string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.allowlist = append(f.allowlist, network)
	f.mu.Unlock()
	return nil
}

// AddDeny adds a CIDR range to the denylist.
func (f *IPFilter) AddDeny(cidr string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.denylist = append(f.denylist, network)
	f.mu.Unlock()
	return nil
}

// Allow returns nil if the IP is permitted.
func (f *IPFilter) Allow(ipStr string) error {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ErrIPDenied
	}
	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, deny := range f.denylist {
		if deny.Contains(ip) {
			return fmt.Errorf("%w: %s is explicitly denied", ErrIPDenied, ipStr)
		}
	}

	if f.mode == "allowlist" && len(f.allowlist) > 0 {
		for _, allow := range f.allowlist {
			if allow.Contains(ip) {
				return nil
			}
		}
		return fmt.Errorf("%w: %s is not in allowlist", ErrIPDenied, ipStr)
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Encryption (AES-256-GCM)
// ────────────────────────────────────────────────────────────────────────────────

// Encryptor provides AES-256-GCM encryption for sensitive fields.
type Encryptor struct {
	key []byte // 32 bytes for AES-256
}

// NewEncryptor creates an Encryptor. key must be exactly 32 bytes.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("security: AES-256 key must be 32 bytes, got %d", len(key))
	}
	k := make([]byte, 32)
	copy(k, key)
	return &Encryptor{key: k}, nil
}

// Encrypt encrypts plaintext and returns base64-encoded ciphertext (nonce+ciphertext).
func (e *Encryptor) Encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", ErrEncryptionFailed
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", ErrEncryptionFailed
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", ErrEncryptionFailed
	}
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt decrypts a base64-encoded ciphertext produced by Encrypt.
func (e *Encryptor) Decrypt(ciphertext string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	if len(data) < gcm.NonceSize() {
		return nil, ErrDecryptionFailed
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

// EncryptString is a convenience wrapper for string plaintext.
func (e *Encryptor) EncryptString(s string) (string, error) { return e.Encrypt([]byte(s)) }

// DecryptString is a convenience wrapper returning a string.
func (e *Encryptor) DecryptString(s string) (string, error) {
	b, err := e.Decrypt(s)
	return string(b), err
}

// ────────────────────────────────────────────────────────────────────────────────
// CSRF Token
// ────────────────────────────────────────────────────────────────────────────────

// CSRFManager generates and validates CSRF tokens.
type CSRFManager struct {
	key []byte
	ttl time.Duration
}

// NewCSRFManager creates a CSRF manager.
func NewCSRFManager(key []byte, ttl time.Duration) *CSRFManager {
	if ttl == 0 {
		ttl = 2 * time.Hour
	}
	return &CSRFManager{key: key, ttl: ttl}
}

// Generate produces a CSRF token for a session.
func (c *CSRFManager) Generate(sessionID string) string {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randomHex(8)
	raw := sessionID + "|" + ts + "|" + nonce
	h := hmac.New(sha256.New, c.key)
	h.Write([]byte(raw))
	sig := hex.EncodeToString(h.Sum(nil))
	return base64.URLEncoding.EncodeToString([]byte(raw + "|" + sig))
}

// Validate verifies a CSRF token.
func (c *CSRFManager) Validate(token, sessionID string) error {
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return ErrTokenInvalid
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 4 {
		return ErrTokenInvalid
	}
	storedSession, ts, nonce, sig := parts[0], parts[1], parts[2], parts[3]
	if storedSession != sessionID {
		return ErrTokenInvalid
	}
	raw := storedSession + "|" + ts + "|" + nonce
	h := hmac.New(sha256.New, c.key)
	h.Write([]byte(raw))
	expected := hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return ErrTokenInvalid
	}
	if err := validateTimestamp(ts, c.ttl); err != nil {
		return ErrTokenExpired
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Suspicious Activity Detection hook
// ────────────────────────────────────────────────────────────────────────────────

// ThreatLevel represents the severity of a detected threat.
type ThreatLevel int

const (
	ThreatNone     ThreatLevel = 0
	ThreatLow      ThreatLevel = 1
	ThreatMedium   ThreatLevel = 2
	ThreatHigh     ThreatLevel = 3
	ThreatCritical ThreatLevel = 4
)

// ThreatEvent is raised when suspicious activity is detected.
type ThreatEvent struct {
	Identity    string
	IPAddress   string
	Level       ThreatLevel
	Description string
	Timestamp   time.Time
	Extra       map[string]string
}

// ThreatDetector monitors for suspicious patterns.
type ThreatDetector struct {
	mu         sync.Mutex
	counters   map[string]*atomic.Int64
	thresholds map[string]int64
	handlers   []func(ThreatEvent)
}

// NewThreatDetector creates a threat detector.
func NewThreatDetector() *ThreatDetector {
	return &ThreatDetector{
		counters:   make(map[string]*atomic.Int64),
		thresholds: make(map[string]int64),
	}
}

// OnThreat registers a handler called on threat detection.
func (d *ThreatDetector) OnThreat(fn func(ThreatEvent)) {
	d.mu.Lock()
	d.handlers = append(d.handlers, fn)
	d.mu.Unlock()
}

// SetThreshold defines how many occurrences trigger a threat event.
func (d *ThreatDetector) SetThreshold(event string, count int64) {
	d.mu.Lock()
	d.thresholds[event] = count
	d.mu.Unlock()
}

// Record increments a counter and emits a threat event if threshold is exceeded.
func (d *ThreatDetector) Record(event, identity, ip string, level ThreatLevel) {
	key := event + ":" + identity
	d.mu.Lock()
	if _, ok := d.counters[key]; !ok {
		d.counters[key] = &atomic.Int64{}
	}
	ctr := d.counters[key]
	threshold, hasThreshold := d.thresholds[event]
	handlers := d.handlers
	d.mu.Unlock()

	count := ctr.Add(1)
	if hasThreshold && count >= threshold {
		te := ThreatEvent{
			Identity:    identity,
			IPAddress:   ip,
			Level:       level,
			Description: fmt.Sprintf("threshold exceeded for event %s: %d occurrences", event, count),
			Timestamp:   time.Now(),
		}
		for _, h := range handlers {
			h(te)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────────
// SecuritySuite — aggregated facade
// ────────────────────────────────────────────────────────────────────────────────

// SuiteConfig configures all security components.
type SuiteConfig struct {
	JWTSigningKey      []byte
	JWTIssuer          string
	JWTTTL             time.Duration
	APIKeyHMACKey      []byte
	RequestSigningKey  []byte
	EncryptionKey      []byte // 32 bytes
	CSRFKey            []byte
	CSRFTTL            time.Duration
	RateLimitCapacity  float64
	RateLimitRate      float64
	LockoutMaxFailures int
	LockoutDuration    time.Duration
}

// Suite bundles all security components.
type Suite struct {
	Tokens         *TokenManager
	APIKeys        *APIKeyManager
	Signer         *RequestSigner
	RateLimiter    *RateLimiter
	Lockout        *BruteForceGuard
	CSRF           *CSRFManager
	Encryptor      *Encryptor
	ThreatDetector *ThreatDetector
	IPFilter       *IPFilter
}

// NewSuite initializes the full security suite.
func NewSuite(cfg SuiteConfig) (*Suite, error) {
	enc, err := NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	return &Suite{
		Tokens:  NewTokenManager(cfg.JWTSigningKey, cfg.JWTIssuer, cfg.JWTTTL),
		APIKeys: NewAPIKeyManager(cfg.APIKeyHMACKey),
		Signer:  NewRequestSigner(cfg.RequestSigningKey),
		RateLimiter: NewRateLimiter(RateLimiterConfig{
			Capacity: cfg.RateLimitCapacity,
			Rate:     cfg.RateLimitRate,
		}),
		Lockout: NewBruteForceGuard(LockoutConfig{
			MaxFailures:     cfg.LockoutMaxFailures,
			LockoutDuration: cfg.LockoutDuration,
		}),
		CSRF:           NewCSRFManager(cfg.CSRFKey, cfg.CSRFTTL),
		Encryptor:      enc,
		ThreatDetector: NewThreatDetector(),
		IPFilter:       NewIPFilter("denylist"),
	}, nil
}

// Authenticate performs the full authentication pipeline:
// rate limit → IP check → lockout check → token validation.
func (s *Suite) Authenticate(ctx context.Context, token, ip, identity string) (*Claims, error) {
	if err := s.IPFilter.Allow(ip); err != nil {
		return nil, err
	}
	if err := s.RateLimiter.Check(identity); err != nil {
		return nil, err
	}
	if err := s.Lockout.Check(identity); err != nil {
		return nil, err
	}
	claims, err := s.Tokens.Validate(token)
	if err != nil {
		s.Lockout.RecordFailure(identity)
		s.ThreatDetector.Record("auth_failure", identity, ip, ThreatMedium)
		return nil, err
	}
	s.Lockout.RecordSuccess(identity)
	return claims, nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────────

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func sanitizeServiceName(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == '-' || r == '_' {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

func parseUnix(s string) (time.Time, error) {
	var ts int64
	_, err := fmt.Sscan(s, &ts)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

func validateTimestamp(tsStr string, maxAge time.Duration) error {
	t, err := parseUnix(tsStr)
	if err != nil {
		return err
	}
	if time.Since(t) > maxAge {
		return ErrTokenExpired
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
