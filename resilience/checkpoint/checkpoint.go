package resilience

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// CheckpointState holds the serializable progress state.
type CheckpointState[K comparable, V any] struct {
	ID          string            `json:"id"`
	Cursor      K                 `json:"cursor"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	ProcessedAt time.Time         `json:"processed_at"`
	Version     int64             `json:"version"`
	Extra       V                 `json:"extra,omitempty"`
}

// CheckpointStore is the persistence backend for checkpoints.
type CheckpointStore[K comparable, V any] interface {
	Save(ctx context.Context, state CheckpointState[K, V]) error
	Load(ctx context.Context, id string) (CheckpointState[K, V], error)
	Delete(ctx context.Context, id string) error
}

// ─────────────────────────────────────────────────────────────────────────────
// FileCheckpointStore: JSON-file-based implementation
// ─────────────────────────────────────────────────────────────────────────────

// FileCheckpointStore persists checkpoint state to a JSON file on disk.
type FileCheckpointStore[K comparable, V any] struct {
	dir string
	mu  sync.Mutex
}

// NewFileCheckpointStore creates a store backed by a directory of JSON files.
func NewFileCheckpointStore[K comparable, V any](dir string) (*FileCheckpointStore[K, V], error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("checkpoint: create dir %s: %w", dir, err)
	}
	return &FileCheckpointStore[K, V]{dir: dir}, nil
}

func (s *FileCheckpointStore[K, V]) path(id string) string {
	return filepath.Join(s.dir, id+".checkpoint.json")
}

func (s *FileCheckpointStore[K, V]) Save(ctx context.Context, state CheckpointState[K, V]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state.ProcessedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint: marshal %s: %w", state.ID, err)
	}

	tmp := s.path(state.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("checkpoint: write tmp %s: %w", state.ID, err)
	}
	if err := os.Rename(tmp, s.path(state.ID)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("checkpoint: rename %s: %w", state.ID, err)
	}
	return nil
}

func (s *FileCheckpointStore[K, V]) Load(ctx context.Context, id string) (CheckpointState[K, V], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return CheckpointState[K, V]{}, fmt.Errorf("checkpoint: not found: %s", id)
		}
		return CheckpointState[K, V]{}, fmt.Errorf("checkpoint: read %s: %w", id, err)
	}
	var state CheckpointState[K, V]
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("checkpoint: unmarshal %s: %w", id, err)
	}
	return state, nil
}

func (s *FileCheckpointStore[K, V]) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("checkpoint: delete %s: %w", id, err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckpointManager: wraps a store with auto-save, WAL semantics, and hooks
// ─────────────────────────────────────────────────────────────────────────────

// CheckpointConfig configures the CheckpointManager.
type CheckpointConfig struct {
	// ID is the unique identifier for this checkpoint stream.
	ID string
	// AutoSaveInterval: if >0, auto-saves state in the background.
	AutoSaveInterval time.Duration
	// SaveThreshold: save after N commits even without auto-save timer.
	SaveThreshold int
	// OnSave is called after each successful save.
	OnSave func(id string, version int64)
	// OnRestore is called after a successful restore.
	OnRestore func(id string, version int64)
	// OnError is called on save/load errors.
	OnError func(id string, op string, err error)
}

// CheckpointManager provides a high-level checkpoint workflow with
// auto-save, versioning, and hooks.
type CheckpointManager[K comparable, V any] struct {
	cfg     CheckpointConfig
	store   CheckpointStore[K, V]
	current CheckpointState[K, V]
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	dirty   atomic.Int64 // commits since last save
	saves   atomic.Int64
	version atomic.Int64
}

// NewCheckpointManager creates a CheckpointManager.
func NewCheckpointManager[K comparable, V any](
	cfg CheckpointConfig,
	store CheckpointStore[K, V],
) *CheckpointManager[K, V] {
	ctx, cancel := context.WithCancel(context.Background())
	cm := &CheckpointManager[K, V]{
		cfg:    cfg,
		store:  store,
		ctx:    ctx,
		cancel: cancel,
	}
	cm.current.ID = cfg.ID
	cm.current.Metadata = make(map[string]string)

	if cfg.AutoSaveInterval > 0 {
		cm.wg.Add(1)
		go cm.autoSaveLoop()
	}
	return cm
}

// Restore loads the last saved state. Returns (state, nil) or error if not found.
func (cm *CheckpointManager[K, V]) Restore(ctx context.Context) (CheckpointState[K, V], error) {
	state, err := cm.store.Load(ctx, cm.cfg.ID)
	if err != nil {
		if cm.cfg.OnError != nil {
			cm.cfg.OnError(cm.cfg.ID, "restore", err)
		}
		return state, err
	}

	cm.mu.Lock()
	cm.current = state
	cm.version.Store(state.Version)
	cm.mu.Unlock()

	if cm.cfg.OnRestore != nil {
		cm.cfg.OnRestore(cm.cfg.ID, state.Version)
	}
	return state, nil
}

// Commit updates the in-memory cursor and optional extra state.
// Triggers an immediate save if SaveThreshold is exceeded.
func (cm *CheckpointManager[K, V]) Commit(cursor K, extra V, meta map[string]string) error {
	cm.mu.Lock()
	cm.current.Cursor = cursor
	cm.current.Extra = extra
	if meta != nil {
		for k, v := range meta {
			cm.current.Metadata[k] = v
		}
	}
	cm.current.Version = cm.version.Add(1)
	cm.mu.Unlock()

	n := cm.dirty.Add(1)
	if cm.cfg.SaveThreshold > 0 && int(n) >= cm.cfg.SaveThreshold {
		return cm.Save(context.Background())
	}
	return nil
}

// Save persists the current state to the store immediately.
func (cm *CheckpointManager[K, V]) Save(ctx context.Context) error {
	cm.mu.RLock()
	state := cm.current
	cm.mu.RUnlock()

	err := cm.store.Save(ctx, state)
	if err != nil {
		if cm.cfg.OnError != nil {
			cm.cfg.OnError(cm.cfg.ID, "save", err)
		}
		return err
	}
	cm.dirty.Store(0)
	cm.saves.Add(1)
	if cm.cfg.OnSave != nil {
		cm.cfg.OnSave(cm.cfg.ID, state.Version)
	}
	return nil
}

// Current returns a snapshot of the current in-memory state.
func (cm *CheckpointManager[K, V]) Current() CheckpointState[K, V] {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.current
}

func (cm *CheckpointManager[K, V]) autoSaveLoop() {
	defer cm.wg.Done()
	ticker := time.NewTicker(cm.cfg.AutoSaveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if cm.dirty.Load() > 0 {
				_ = cm.Save(cm.ctx)
			}
		case <-cm.ctx.Done():
			// Final save on shutdown
			_ = cm.Save(context.Background())
			return
		}
	}
}

// Close stops the auto-save loop and performs a final save.
func (cm *CheckpointManager[K, V]) Close() error {
	cm.cancel()
	cm.wg.Wait()
	return nil
}

// Reset deletes the stored checkpoint.
func (cm *CheckpointManager[K, V]) Reset(ctx context.Context) error {
	return cm.store.Delete(ctx, cm.cfg.ID)
}

// Stats returns runtime metrics.
func (cm *CheckpointManager[K, V]) Stats() CheckpointStats {
	return CheckpointStats{
		ID:      cm.cfg.ID,
		Version: cm.version.Load(),
		Saves:   cm.saves.Load(),
		Dirty:   cm.dirty.Load(),
	}
}

// CheckpointStats is a point-in-time snapshot.
type CheckpointStats struct {
	ID      string
	Version int64
	Saves   int64
	Dirty   int64
}
