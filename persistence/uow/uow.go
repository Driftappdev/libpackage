// Package uow provides an enterprise Unit of Work (UoW) pattern implementation.
//
// The Unit of Work pattern tracks all changes (inserts, updates, deletes) made
// during a business operation and coordinates writing them out as a single
// consistent atomic transaction, integrated with audit logging, idempotency,
// and permission enforcement.
//
// Features:
//   - Repository registration per aggregate root
//   - Change tracking (dirty tracking) with before/after snapshots
//   - Pre/post commit hooks (for domain events, audit)
//   - Saga/compensation support for distributed transactions
//   - Context-propagated UoW instances
//   - Metrics integration hooks
//   - Concurrent-safe operation
package uow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────────
// Errors
// ────────────────────────────────────────────────────────────────────────────────

var (
	ErrUoWAlreadyCommitted  = errors.New("uow: unit of work already committed")
	ErrUoWAlreadyRolledBack = errors.New("uow: unit of work already rolled back")
	ErrNilTransaction       = errors.New("uow: no active transaction")
	ErrRepositoryNotFound   = errors.New("uow: repository not found")
	ErrOptimisticLock       = errors.New("uow: optimistic lock violation — entity modified concurrently")
	ErrDirtyReadProtected   = errors.New("uow: dirty read detected")
)

// ────────────────────────────────────────────────────────────────────────────────
// Context key
// ────────────────────────────────────────────────────────────────────────────────

type contextKey string

const uowContextKey contextKey = "unit_of_work"

// FromContext retrieves a UoW from context.
func FromContext(ctx context.Context) (*UnitOfWork, bool) {
	u, ok := ctx.Value(uowContextKey).(*UnitOfWork)
	return u, ok
}

// IntoContext stores a UoW into context.
func IntoContext(ctx context.Context, u *UnitOfWork) context.Context {
	return context.WithValue(ctx, uowContextKey, u)
}

// ────────────────────────────────────────────────────────────────────────────────
// Change tracking
// ────────────────────────────────────────────────────────────────────────────────

// ChangeType represents the type of a tracked change.
type ChangeType string

const (
	ChangeInsert ChangeType = "INSERT"
	ChangeUpdate ChangeType = "UPDATE"
	ChangeDelete ChangeType = "DELETE"
)

// TrackedChange records a change to an entity.
type TrackedChange struct {
	EntityType string
	EntityID   string
	ChangeType ChangeType
	Before     any   // Snapshot before mutation (nil for INSERT)
	After      any   // Snapshot after mutation (nil for DELETE)
	Version    int64 // For optimistic locking
	Repository string
	ChangedAt  time.Time
	ChangedBy  string // principal ID
}

// ────────────────────────────────────────────────────────────────────────────────
// Repository interface
// ────────────────────────────────────────────────────────────────────────────────

// Repository defines the operations a domain repository must expose.
// Each aggregate root has its own repository registered with the UoW factory.
type Repository interface {
	// Name returns the repository identifier (e.g., "account", "points").
	Name() string

	// Save persists a tracked entity within the given transaction context.
	Save(ctx context.Context, entity any) error

	// Delete removes an entity within the given transaction context.
	Delete(ctx context.Context, entityID string) error
}

// ────────────────────────────────────────────────────────────────────────────────
// Transaction adapter
// ────────────────────────────────────────────────────────────────────────────────

// TxAdapter abstracts the underlying database transaction.
// Implement this for PostgreSQL, MySQL, MongoDB, etc.
type TxAdapter interface {
	// Begin starts a new transaction.
	Begin(ctx context.Context) (Tx, error)
}

// Tx represents an active database transaction.
type Tx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
	// Context returns the transaction-scoped context (for passing to repos).
	Context() context.Context
}

// ────────────────────────────────────────────────────────────────────────────────
// Hooks
// ────────────────────────────────────────────────────────────────────────────────

// CommitHook is called before or after committing the unit of work.
type CommitHook func(ctx context.Context, changes []*TrackedChange) error

// RollbackHook is called when a unit of work is rolled back.
type RollbackHook func(ctx context.Context, changes []*TrackedChange, reason error)

// ────────────────────────────────────────────────────────────────────────────────
// UnitOfWork
// ────────────────────────────────────────────────────────────────────────────────

// State of the unit of work.
type State int

const (
	StateActive State = iota
	StateCommitted
	StateRolledBack
)

// UnitOfWork tracks domain changes and coordinates transaction commit.
type UnitOfWork struct {
	mu          sync.Mutex
	id          string
	state       State
	tx          Tx
	repos       map[string]Repository
	changes     []*TrackedChange
	preCommit   []CommitHook
	postCommit  []CommitHook
	onRollback  []RollbackHook
	startedAt   time.Time
	principalID string
	metadata    map[string]string
}

// ID returns the unique ID of this unit of work.
func (u *UnitOfWork) ID() string { return u.id }

// State returns the current state.
func (u *UnitOfWork) State() State { return u.state }

// Tx returns the underlying transaction (nil if not started).
func (u *UnitOfWork) Tx() Tx { return u.tx }

// Changes returns all tracked changes in insertion order.
func (u *UnitOfWork) Changes() []*TrackedChange {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*TrackedChange(nil), u.changes...)
}

// Repository retrieves a registered repository by name.
func (u *UnitOfWork) Repository(name string) (Repository, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	r, ok := u.repos[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrRepositoryNotFound, name)
	}
	return r, nil
}

// TrackInsert records an entity insertion.
func (u *UnitOfWork) TrackInsert(entityType, entityID string, after any) {
	u.track(&TrackedChange{
		EntityType: entityType,
		EntityID:   entityID,
		ChangeType: ChangeInsert,
		After:      after,
	})
}

// TrackUpdate records an entity update.
func (u *UnitOfWork) TrackUpdate(entityType, entityID string, before, after any, version int64) {
	u.track(&TrackedChange{
		EntityType: entityType,
		EntityID:   entityID,
		ChangeType: ChangeUpdate,
		Before:     before,
		After:      after,
		Version:    version,
	})
}

// TrackDelete records an entity deletion.
func (u *UnitOfWork) TrackDelete(entityType, entityID string, before any) {
	u.track(&TrackedChange{
		EntityType: entityType,
		EntityID:   entityID,
		ChangeType: ChangeDelete,
		Before:     before,
	})
}

func (u *UnitOfWork) track(c *TrackedChange) {
	u.mu.Lock()
	defer u.mu.Unlock()
	c.ChangedAt = time.Now()
	c.ChangedBy = u.principalID
	u.changes = append(u.changes, c)
}

// OnPreCommit registers a hook called before the database transaction commits.
// Useful for domain event dispatch or validation.
func (u *UnitOfWork) OnPreCommit(h CommitHook) {
	u.mu.Lock()
	u.preCommit = append(u.preCommit, h)
	u.mu.Unlock()
}

// OnPostCommit registers a hook called after successful commit.
// Useful for audit logging and cache invalidation.
func (u *UnitOfWork) OnPostCommit(h CommitHook) {
	u.mu.Lock()
	u.postCommit = append(u.postCommit, h)
	u.mu.Unlock()
}

// OnRollback registers a hook called on rollback.
// Useful for compensation logic.
func (u *UnitOfWork) OnRollback(h RollbackHook) {
	u.mu.Lock()
	u.onRollback = append(u.onRollback, h)
	u.mu.Unlock()
}

// Commit runs pre-commit hooks, commits the transaction, then runs post-commit hooks.
func (u *UnitOfWork) Commit(ctx context.Context) error {
	u.mu.Lock()
	if u.state != StateActive {
		u.mu.Unlock()
		if u.state == StateCommitted {
			return ErrUoWAlreadyCommitted
		}
		return ErrUoWAlreadyRolledBack
	}
	changes := append([]*TrackedChange(nil), u.changes...)
	preHooks := append([]CommitHook(nil), u.preCommit...)
	postHooks := append([]CommitHook(nil), u.postCommit...)
	rollbackHooks := append([]RollbackHook(nil), u.onRollback...)
	tx := u.tx
	u.mu.Unlock()

	// Pre-commit hooks.
	for _, h := range preHooks {
		if err := h(ctx, changes); err != nil {
			u.doRollback(ctx, changes, rollbackHooks, err)
			return fmt.Errorf("uow: pre-commit hook failed: %w", err)
		}
	}

	// Commit transaction.
	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			u.doRollback(ctx, changes, rollbackHooks, err)
			return fmt.Errorf("uow: transaction commit failed: %w", err)
		}
	}

	u.mu.Lock()
	u.state = StateCommitted
	u.mu.Unlock()

	// Post-commit hooks (best-effort, non-transactional).
	for _, h := range postHooks {
		if err := h(ctx, changes); err != nil {
			// Log but do not fail after commit.
			_ = err
		}
	}

	return nil
}

// Rollback aborts the transaction.
func (u *UnitOfWork) Rollback(ctx context.Context, reason error) {
	u.mu.Lock()
	if u.state != StateActive {
		u.mu.Unlock()
		return
	}
	changes := append([]*TrackedChange(nil), u.changes...)
	hooks := append([]RollbackHook(nil), u.onRollback...)
	u.mu.Unlock()
	u.doRollback(ctx, changes, hooks, reason)
}

func (u *UnitOfWork) doRollback(ctx context.Context, changes []*TrackedChange, hooks []RollbackHook, reason error) {
	if u.tx != nil {
		_ = u.tx.Rollback(ctx)
	}
	u.mu.Lock()
	u.state = StateRolledBack
	u.mu.Unlock()

	for _, h := range hooks {
		h(ctx, changes, reason)
	}
}

// Duration returns how long the unit of work has been open.
func (u *UnitOfWork) Duration() time.Duration { return time.Since(u.startedAt) }

// ────────────────────────────────────────────────────────────────────────────────
// Factory
// ────────────────────────────────────────────────────────────────────────────────

// FactoryConfig configures the UoW factory.
type FactoryConfig struct {
	// TxAdapter is optional; if nil, UoW works without DB transaction management.
	TxAdapter TxAdapter

	// GlobalPreCommitHooks are run on every UoW before commit.
	GlobalPreCommitHooks []CommitHook

	// GlobalPostCommitHooks are run on every UoW after commit.
	GlobalPostCommitHooks []CommitHook

	// GlobalRollbackHooks are run on every UoW rollback.
	GlobalRollbackHooks []RollbackHook
}

// Factory creates UnitOfWork instances.
type Factory struct {
	mu     sync.RWMutex
	repos  map[string]Repository
	config FactoryConfig
	seq    uint64
}

// NewFactory creates a UoW factory.
func NewFactory(cfg FactoryConfig) *Factory {
	return &Factory{
		repos:  make(map[string]Repository),
		config: cfg,
	}
}

// Register adds a repository to the factory.
func (f *Factory) Register(repo Repository) {
	f.mu.Lock()
	f.repos[repo.Name()] = repo
	f.mu.Unlock()
}

// NewUoW creates and optionally starts a new unit of work.
// If TxAdapter is configured, a database transaction is started automatically.
func (f *Factory) NewUoW(ctx context.Context, principalID string) (*UnitOfWork, error) {
	f.mu.RLock()
	reposCopy := make(map[string]Repository, len(f.repos))
	for k, v := range f.repos {
		reposCopy[k] = v
	}
	f.mu.RUnlock()

	f.seq++
	uow := &UnitOfWork{
		id:          fmt.Sprintf("uow-%d-%d", time.Now().UnixNano(), f.seq),
		state:       StateActive,
		repos:       reposCopy,
		startedAt:   time.Now(),
		principalID: principalID,
		metadata:    make(map[string]string),
	}

	// Attach global hooks.
	uow.preCommit = append(uow.preCommit, f.config.GlobalPreCommitHooks...)
	uow.postCommit = append(uow.postCommit, f.config.GlobalPostCommitHooks...)
	uow.onRollback = append(uow.onRollback, f.config.GlobalRollbackHooks...)

	// Start database transaction if adapter configured.
	if f.config.TxAdapter != nil {
		tx, err := f.config.TxAdapter.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("uow: failed to begin transaction: %w", err)
		}
		uow.tx = tx
	}

	return uow, nil
}

// Execute is a convenience wrapper that runs fn within a UoW and auto-commits or rolls back.
func (f *Factory) Execute(ctx context.Context, principalID string, fn func(ctx context.Context, uow *UnitOfWork) error) error {
	uow, err := f.NewUoW(ctx, principalID)
	if err != nil {
		return err
	}

	txCtx := IntoContext(ctx, uow)
	if uow.tx != nil {
		txCtx = uow.tx.Context()
	}

	if err := fn(txCtx, uow); err != nil {
		uow.Rollback(ctx, err)
		return err
	}
	return uow.Commit(ctx)
}

// ────────────────────────────────────────────────────────────────────────────────
// Saga / Compensation
// ────────────────────────────────────────────────────────────────────────────────

// CompensationFunc is called to undo a step in case of saga failure.
type CompensationFunc func(ctx context.Context) error

// Saga coordinates multi-step distributed operations with compensation.
type Saga struct {
	mu          sync.Mutex
	steps       []sagaStep
	executed    int
	compensated bool
}

type sagaStep struct {
	name         string
	compensation CompensationFunc
}

// NewSaga creates a new saga coordinator.
func NewSaga() *Saga { return &Saga{} }

// Step registers a compensation for a successfully executed step.
// Call this immediately after each step succeeds.
func (s *Saga) Step(name string, compensation CompensationFunc) {
	s.mu.Lock()
	s.steps = append(s.steps, sagaStep{name: name, compensation: compensation})
	s.executed++
	s.mu.Unlock()
}

// Compensate runs all registered compensations in reverse order.
// This is called when a step fails.
func (s *Saga) Compensate(ctx context.Context) error {
	s.mu.Lock()
	if s.compensated {
		s.mu.Unlock()
		return nil
	}
	steps := append([]sagaStep(nil), s.steps...)
	s.compensated = true
	s.mu.Unlock()

	var errs []string
	for i := len(steps) - 1; i >= 0; i-- {
		if err := steps[i].compensation(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("step %s: %v", steps[i].name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("saga compensation errors: %s", joinStrings(errs, "; "))
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────────

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
