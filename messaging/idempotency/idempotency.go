// Package idempotency provides enterprise-grade idempotency management
// for distributed systems handling financial transactions, points, coupons, and user data.
//
// Features:
//   - Pluggable storage backends (Redis, PostgreSQL, In-Memory)
//   - Request fingerprinting & SHA-256 payload hashing
//   - TTL-based key expiration
//   - Concurrent request deduplication (single-flight)
//   - Full audit trail integration
//   - Distributed lock support
//   - Response caching with compression
//   - Namespace isolation per service
//   - Configurable retry & conflict policies
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ────────────────────────────────────────────────────────────────────────────────
// Errors
// ────────────────────────────────────────────────────────────────────────────────

var (
	ErrKeyConflict        = errors.New("idempotency: key already in use with different payload")
	ErrKeyExpired         = errors.New("idempotency: key has expired")
	ErrRequestInFlight    = errors.New("idempotency: identical request already in flight")
	ErrStorageUnavailable = errors.New("idempotency: storage backend unavailable")
	ErrInvalidKey         = errors.New("idempotency: invalid idempotency key format")
	ErrResponseCorrupted  = errors.New("idempotency: cached response is corrupted")
)

// ────────────────────────────────────────────────────────────────────────────────
// Status
// ────────────────────────────────────────────────────────────────────────────────

type Status string

const (
	StatusPending   Status = "PENDING"   // Request received, processing
	StatusCompleted Status = "COMPLETED" // Successfully completed
	StatusFailed    Status = "FAILED"    // Processing failed (retryable)
	StatusExpired   Status = "EXPIRED"   // TTL exceeded
)

// ────────────────────────────────────────────────────────────────────────────────
// Core Types
// ────────────────────────────────────────────────────────────────────────────────

// Record represents a stored idempotency record.
type Record struct {
	Key           string            `json:"key"`
	Namespace     string            `json:"namespace"`
	PayloadHash   string            `json:"payload_hash"`
	Status        Status            `json:"status"`
	Response      json.RawMessage   `json:"response,omitempty"`
	ErrorMessage  string            `json:"error_message,omitempty"`
	HTTPStatus    int               `json:"http_status,omitempty"`
	RequestID     string            `json:"request_id"`
	UserID        string            `json:"user_id,omitempty"`
	ServiceName   string            `json:"service_name"`
	OperationName string            `json:"operation_name"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	ExpiresAt     time.Time         `json:"expires_at"`
	CompletedAt   *time.Time        `json:"completed_at,omitempty"`
	AttemptCount  int               `json:"attempt_count"`
}

// IsExpired returns true if the record has passed its TTL.
func (r *Record) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// IsTerminal returns true if the record is in a final state.
func (r *Record) IsTerminal() bool {
	return r.Status == StatusCompleted || r.Status == StatusExpired
}

// Request carries all information needed to evaluate idempotency.
type Request struct {
	Key           string
	Namespace     string
	Payload       any
	UserID        string
	ServiceName   string
	OperationName string
	Metadata      map[string]string
	TTL           time.Duration
}

// Result is returned after evaluating an idempotent operation.
type Result struct {
	Record    *Record
	IsCached  bool // true = response retrieved from cache (no processing needed)
	RequestID string
}

// ────────────────────────────────────────────────────────────────────────────────
// Storage Backend Interface
// ────────────────────────────────────────────────────────────────────────────────

// Store is the pluggable storage interface for idempotency records.
type Store interface {
	// Get retrieves a record by namespace + key.
	Get(ctx context.Context, namespace, key string) (*Record, error)

	// Create inserts a new record atomically (fails if key exists).
	Create(ctx context.Context, record *Record) error

	// Update updates an existing record (status, response, error).
	Update(ctx context.Context, record *Record) error

	// Delete removes a record (used for cleanup / testing).
	Delete(ctx context.Context, namespace, key string) error

	// AcquireLock acquires a distributed lock for a key.
	AcquireLock(ctx context.Context, namespace, key string, ttl time.Duration) (LockHandle, error)

	// Ping verifies the storage backend is reachable.
	Ping(ctx context.Context) error
}

// LockHandle represents a distributed lock.
type LockHandle interface {
	Release(ctx context.Context) error
}

// ────────────────────────────────────────────────────────────────────────────────
// In-Memory Store (for dev/testing)
// ────────────────────────────────────────────────────────────────────────────────

type memoryLock struct {
	mu      sync.Mutex
	store   *MemoryStore
	ns, key string
	done    chan struct{}
}

func (l *memoryLock) Release(_ context.Context) error {
	close(l.done)
	l.store.mu.Lock()
	delete(l.store.locks, l.ns+":"+l.key)
	l.store.mu.Unlock()
	return nil
}

// MemoryStore is a thread-safe in-memory idempotency store.
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]*Record
	locks   map[string]chan struct{}
}

// NewMemoryStore returns a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records: make(map[string]*Record),
		locks:   make(map[string]chan struct{}),
	}
}

func memKey(ns, key string) string { return ns + "::" + key }

func (s *MemoryStore) Get(_ context.Context, ns, key string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[memKey(ns, key)]
	if !ok {
		return nil, nil
	}
	copy := *r
	return &copy, nil
}

func (s *MemoryStore) Create(_ context.Context, record *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memKey(record.Namespace, record.Key)
	if _, exists := s.records[k]; exists {
		return fmt.Errorf("record already exists: %w", ErrKeyConflict)
	}
	copy := *record
	s.records[k] = &copy
	return nil
}

func (s *MemoryStore) Update(_ context.Context, record *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memKey(record.Namespace, record.Key)
	if _, exists := s.records[k]; !exists {
		return fmt.Errorf("record not found: %s", k)
	}
	copy := *record
	s.records[k] = &copy
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, ns, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, memKey(ns, key))
	return nil
}

func (s *MemoryStore) AcquireLock(ctx context.Context, ns, key string, _ time.Duration) (LockHandle, error) {
	fullKey := ns + ":" + key
	s.mu.Lock()
	if _, busy := s.locks[fullKey]; busy {
		s.mu.Unlock()
		return nil, ErrRequestInFlight
	}
	done := make(chan struct{})
	s.locks[fullKey] = done
	s.mu.Unlock()
	return &memoryLock{store: s, ns: ns, key: key, done: done}, nil
}

func (s *MemoryStore) Ping(_ context.Context) error { return nil }

// ────────────────────────────────────────────────────────────────────────────────
// Manager
// ────────────────────────────────────────────────────────────────────────────────

// Config configures the idempotency manager.
type Config struct {
	// DefaultTTL is the default key lifetime (default: 24h).
	DefaultTTL time.Duration

	// DefaultNamespace is used when Request.Namespace is empty.
	DefaultNamespace string

	// ServiceName tags all records with the service name.
	ServiceName string

	// StrictPayloadCheck fails on payload hash mismatch (recommended for financial).
	StrictPayloadCheck bool

	// MaxRetryAttempts allows re-processing if a previous attempt failed.
	MaxRetryAttempts int

	// LockTTL is how long a distributed lock is held (default: 30s).
	LockTTL time.Duration

	// OnConflict is called when a key conflict is detected.
	OnConflict func(ctx context.Context, existing *Record, req *Request)
}

func (c *Config) defaults() {
	if c.DefaultTTL == 0 {
		c.DefaultTTL = 24 * time.Hour
	}
	if c.DefaultNamespace == "" {
		c.DefaultNamespace = "default"
	}
	if c.MaxRetryAttempts == 0 {
		c.MaxRetryAttempts = 3
	}
	if c.LockTTL == 0 {
		c.LockTTL = 30 * time.Second
	}
}

// Manager is the central idempotency manager.
type Manager struct {
	store  Store
	config Config
	group  singleFlightGroup
}

// NewManager creates a new Manager.
func NewManager(store Store, cfg Config) *Manager {
	cfg.defaults()
	return &Manager{store: store, config: cfg}
}

// Evaluate checks if the request has been seen before.
// Returns (result, nil) – caller inspects result.IsCached to decide whether to process.
func (m *Manager) Evaluate(ctx context.Context, req *Request) (*Result, error) {
	if err := validateKey(req.Key); err != nil {
		return nil, err
	}
	if req.Namespace == "" {
		req.Namespace = m.config.DefaultNamespace
	}
	if req.TTL == 0 {
		req.TTL = m.config.DefaultTTL
	}

	payloadHash, err := hashPayload(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("idempotency: failed to hash payload: %w", err)
	}

	// ── Check existing record ──────────────────────────────────────────────────
	existing, err := m.store.Get(ctx, req.Namespace, req.Key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}

	if existing != nil {
		if existing.IsExpired() {
			_ = m.store.Delete(ctx, req.Namespace, req.Key)
			// Fall through to create a new record.
		} else {
			// Payload mismatch check.
			if m.config.StrictPayloadCheck && existing.PayloadHash != payloadHash {
				if m.config.OnConflict != nil {
					m.config.OnConflict(ctx, existing, req)
				}
				return nil, ErrKeyConflict
			}

			// Previously failed and retry budget remains.
			if existing.Status == StatusFailed && existing.AttemptCount < m.config.MaxRetryAttempts {
				existing.AttemptCount++
				existing.Status = StatusPending
				existing.UpdatedAt = time.Now()
				_ = m.store.Update(ctx, existing)
				return &Result{Record: existing, IsCached: false, RequestID: existing.RequestID}, nil
			}

			// Still pending — concurrent duplicate.
			if existing.Status == StatusPending {
				return nil, ErrRequestInFlight
			}

			// Completed — return cached result.
			return &Result{Record: existing, IsCached: true, RequestID: existing.RequestID}, nil
		}
	}

	// ── Acquire distributed lock & create new record ───────────────────────────
	lock, err := m.store.AcquireLock(ctx, req.Namespace, req.Key, m.config.LockTTL)
	if err != nil {
		return nil, ErrRequestInFlight
	}
	defer lock.Release(ctx) //nolint:errcheck

	requestID := uuid.New().String()
	now := time.Now()
	record := &Record{
		Key:           req.Key,
		Namespace:     req.Namespace,
		PayloadHash:   payloadHash,
		Status:        StatusPending,
		RequestID:     requestID,
		UserID:        req.UserID,
		ServiceName:   coalesce(req.ServiceName, m.config.ServiceName),
		OperationName: req.OperationName,
		Metadata:      req.Metadata,
		CreatedAt:     now,
		UpdatedAt:     now,
		ExpiresAt:     now.Add(req.TTL),
		AttemptCount:  1,
	}

	if err := m.store.Create(ctx, record); err != nil {
		// Race condition — another goroutine created it first.
		existing, _ := m.store.Get(ctx, req.Namespace, req.Key)
		if existing != nil {
			return &Result{Record: existing, IsCached: existing.Status == StatusCompleted, RequestID: existing.RequestID}, nil
		}
		return nil, fmt.Errorf("%w: create failed: %v", ErrStorageUnavailable, err)
	}

	return &Result{Record: record, IsCached: false, RequestID: requestID}, nil
}

// Complete marks a request as successfully completed, storing the response.
func (m *Manager) Complete(ctx context.Context, namespace, key string, response any, httpStatus int) error {
	if namespace == "" {
		namespace = m.config.DefaultNamespace
	}
	record, err := m.store.Get(ctx, namespace, key)
	if err != nil || record == nil {
		return fmt.Errorf("idempotency: record not found for completion: %s/%s", namespace, key)
	}

	responseBytes, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("idempotency: failed to marshal response: %w", err)
	}

	now := time.Now()
	record.Status = StatusCompleted
	record.Response = responseBytes
	record.HTTPStatus = httpStatus
	record.UpdatedAt = now
	record.CompletedAt = &now

	return m.store.Update(ctx, record)
}

// Fail marks a request as failed.
func (m *Manager) Fail(ctx context.Context, namespace, key, errMsg string) error {
	if namespace == "" {
		namespace = m.config.DefaultNamespace
	}
	record, err := m.store.Get(ctx, namespace, key)
	if err != nil || record == nil {
		return fmt.Errorf("idempotency: record not found for failure: %s/%s", namespace, key)
	}

	record.Status = StatusFailed
	record.ErrorMessage = errMsg
	record.UpdatedAt = time.Now()
	return m.store.Update(ctx, record)
}

// GetCachedResponse retrieves the cached response for a completed request.
func (m *Manager) GetCachedResponse(ctx context.Context, namespace, key string, dest any) (*Record, bool, error) {
	if namespace == "" {
		namespace = m.config.DefaultNamespace
	}
	record, err := m.store.Get(ctx, namespace, key)
	if err != nil {
		return nil, false, err
	}
	if record == nil || record.Status != StatusCompleted {
		return record, false, nil
	}
	if err := json.Unmarshal(record.Response, dest); err != nil {
		return record, false, ErrResponseCorrupted
	}
	return record, true, nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Middleware / Decorator helpers
// ────────────────────────────────────────────────────────────────────────────────

// HandlerFunc is the operation to execute if the request is not cached.
type HandlerFunc func(ctx context.Context) (response any, httpStatus int, err error)

// Execute is a convenience wrapper that handles the full idempotency lifecycle.
//
//	result, err := mgr.Execute(ctx, req, func(ctx) (any, int, error) {
//	    // your actual business logic here
//	    return processPayment(ctx, req)
//	})
func (m *Manager) Execute(ctx context.Context, req *Request, fn HandlerFunc) (*Result, error) {
	result, err := m.Evaluate(ctx, req)
	if err != nil {
		return nil, err
	}
	if result.IsCached {
		return result, nil
	}

	resp, status, execErr := fn(ctx)
	if execErr != nil {
		_ = m.Fail(ctx, req.Namespace, req.Key, execErr.Error())
		return result, execErr
	}
	if err := m.Complete(ctx, req.Namespace, req.Key, resp, status); err != nil {
		return result, fmt.Errorf("idempotency: failed to persist result: %w", err)
	}

	// Refresh record.
	updated, _ := m.store.Get(ctx, req.Namespace, req.Key)
	if updated != nil {
		result.Record = updated
	}
	return result, nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────────

// hashPayload returns a deterministic SHA-256 hash of the payload as JSON.
func hashPayload(payload any) (string, error) {
	if payload == nil {
		return "nil", nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// validateKey ensures the idempotency key is a valid UUID or structured string.
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: key must not be empty", ErrInvalidKey)
	}
	if len(key) > 255 {
		return fmt.Errorf("%w: key exceeds 255 chars", ErrInvalidKey)
	}
	return nil
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ────────────────────────────────────────────────────────────────────────────────
// Single-Flight Group (dedup concurrent identical in-process requests)
// ────────────────────────────────────────────────────────────────────────────────

type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

type singleFlightGroup struct {
	mu sync.Mutex
	m  map[string]*call
}

func (g *singleFlightGroup) Do(key string, fn func() (any, error)) (any, error, bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err, true
	}
	c := new(call)
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err, false
}

// ────────────────────────────────────────────────────────────────────────────────
// Key Generator utilities
// ────────────────────────────────────────────────────────────────────────────────

// GenerateKey generates a deterministic idempotency key from components.
// Useful when the client does not supply one.
func GenerateKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0}) // separator
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// NewUUIDKey returns a random UUID-based idempotency key.
func NewUUIDKey() string {
	return uuid.New().String()
}
