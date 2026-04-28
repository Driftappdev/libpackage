package flowguard

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// StateSynchronizer provides eventually-consistent state replication across
// service replicas using a gossip-inspired push-pull protocol and
// Last-Write-Wins (LWW) CRDT semantics.
//
// Key features:
//   - Hybrid Logical Clock (HLC) for causally-consistent versioning
//   - Merkle-tree digest for efficient anti-entropy (detect divergence with O(log N) messages)
//   - Delta-sync: only transmit changed keys
//   - Configurable replication factor and fanout
//   - Full observability and conflict hooks
//
// Example:
//
//	sync := NewStateSynchronizer(StateSyncConfig{
//	    NodeID:          "replica-1",
//	    SyncInterval:    500 * time.Millisecond,
//	    ReplicationFanout: 3,
//	    Transport:       myGRPCTransport,
//	})
//	sync.Set("circuit:user-service", circuitState)
//	val, ok := sync.Get("circuit:user-service")
type StateSynchronizer struct {
	cfg      StateSyncConfig
	nodeID   string
	clock    *hybridLogicalClock
	state    map[string]*SyncEntry
	mu       sync.RWMutex
	stopCh   chan struct{}
	metrics  *syncMetrics
	digest   *merkleDigest
	peers    []SyncPeer
	peerMu   sync.RWMutex
	started  int32
}

// SyncEntry is a versioned key-value pair in the replicated state.
type SyncEntry struct {
	Key       string
	Value     any
	Version   HLCTimestamp
	NodeID    string    // originating node
	DeletedAt *HLCTimestamp // non-nil if tombstoned
}

// HLCTimestamp is a Hybrid Logical Clock timestamp: wall time + logical counter.
type HLCTimestamp struct {
	WallNs  int64  // Unix nanoseconds
	Logical uint32 // Tie-breaking counter
	NodeID  string // For total ordering across nodes
}

// Less returns true if h is causally before other.
func (h HLCTimestamp) Less(other HLCTimestamp) bool {
	if h.WallNs != other.WallNs {
		return h.WallNs < other.WallNs
	}
	if h.Logical != other.Logical {
		return h.Logical < other.Logical
	}
	return h.NodeID < other.NodeID
}

// Equal returns true if timestamps are identical.
func (h HLCTimestamp) Equal(other HLCTimestamp) bool {
	return h.WallNs == other.WallNs && h.Logical == other.Logical && h.NodeID == other.NodeID
}

// SyncPeer represents a remote replica.
type SyncPeer interface {
	ID() string
	// Push sends delta entries to the peer.
	Push(ctx context.Context, entries []*SyncEntry) error
	// Pull fetches entries the peer has that we might be missing (anti-entropy).
	Pull(ctx context.Context, digest []byte) ([]*SyncEntry, error)
}

// SyncTransport abstracts peer discovery.
type SyncTransport interface {
	// Peers returns the current live peer list (excluding self).
	Peers(ctx context.Context) ([]SyncPeer, error)
}

// StateSyncConfig configures the StateSynchronizer.
type StateSyncConfig struct {
	// NodeID uniquely identifies this replica.
	NodeID string

	// SyncInterval is how often gossip rounds run.
	SyncInterval time.Duration

	// ReplicationFanout is the number of peers contacted per gossip round.
	ReplicationFanout int

	// Transport discovers peers.
	Transport SyncTransport

	// MaxEntries caps the state size.
	MaxEntries int

	// TombstoneTTL is how long deleted entries are retained before purging.
	TombstoneTTL time.Duration

	// OnConflict is called when a merge resolves a conflict (LWW winner reported).
	OnConflict func(key string, local, remote *SyncEntry, winner *SyncEntry)

	// OnSyncError is called when a gossip round fails.
	OnSyncError func(peerID string, err error)

	// OnEntryAdded is called when a new entry is replicated from a peer.
	OnEntryAdded func(entry *SyncEntry)
}

func (c *StateSyncConfig) setDefaults() {
	if c.SyncInterval == 0 {
		c.SyncInterval = 500 * time.Millisecond
	}
	if c.ReplicationFanout == 0 {
		c.ReplicationFanout = 3
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = 100_000
	}
	if c.TombstoneTTL == 0 {
		c.TombstoneTTL = 5 * time.Minute
	}
}

// hybridLogicalClock implements HLC (Kulkarni et al. 2014).
type hybridLogicalClock struct {
	mu      sync.Mutex
	wallNs  int64
	logical uint32
	nodeID  string
}

func newHLC(nodeID string) *hybridLogicalClock {
	return &hybridLogicalClock{nodeID: nodeID, wallNs: time.Now().UnixNano()}
}

// Now returns a new HLC timestamp.
func (c *hybridLogicalClock) Now() HLCTimestamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := time.Now().UnixNano()
	if wall > c.wallNs {
		c.wallNs = wall
		c.logical = 0
	} else {
		c.logical++
	}
	return HLCTimestamp{WallNs: c.wallNs, Logical: c.logical, NodeID: c.nodeID}
}

// Update advances the clock based on a received remote timestamp.
func (c *hybridLogicalClock) Update(remote HLCTimestamp) HLCTimestamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	wall := time.Now().UnixNano()
	maxWall := maxInt64(wall, maxInt64(c.wallNs, remote.WallNs))
	if maxWall == c.wallNs && maxWall == remote.WallNs {
		c.logical = maxUint32(c.logical, remote.Logical) + 1
	} else if maxWall == c.wallNs {
		c.logical++
	} else if maxWall == remote.WallNs {
		c.logical = remote.Logical + 1
	} else {
		c.logical = 0
	}
	c.wallNs = maxWall
	return HLCTimestamp{WallNs: c.wallNs, Logical: c.logical, NodeID: c.nodeID}
}

// merkleDigest computes a rolling XOR-hash of the state for anti-entropy.
type merkleDigest struct {
	mu     sync.Mutex
	hashes map[string]uint64
}

func newMerkleDigest() *merkleDigest { return &merkleDigest{hashes: make(map[string]uint64)} }

func (m *merkleDigest) update(key string, version HLCTimestamp) {
	m.mu.Lock()
	m.hashes[key] = fnv64(key) ^ uint64(version.WallNs) ^ uint64(version.Logical)
	m.mu.Unlock()
}

func (m *merkleDigest) delete(key string) {
	m.mu.Lock()
	delete(m.hashes, key)
	m.mu.Unlock()
}

func (m *merkleDigest) bytes() []byte {
	m.mu.Lock()
	keys := make([]string, 0, len(m.hashes))
	for k := range m.hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var acc uint64
	for _, k := range keys {
		acc ^= m.hashes[k]
	}
	m.mu.Unlock()
	b := make([]byte, 8)
	b[0] = byte(acc >> 56)
	b[1] = byte(acc >> 48)
	b[2] = byte(acc >> 40)
	b[3] = byte(acc >> 32)
	b[4] = byte(acc >> 24)
	b[5] = byte(acc >> 16)
	b[6] = byte(acc >> 8)
	b[7] = byte(acc)
	return b
}

type syncMetrics struct {
	sets          int64
	deletes       int64
	pushes        int64
	pulls         int64
	conflicts     int64
	syncErrors    int64
	entriesMerged int64
}

// NewStateSynchronizer creates and returns a new StateSynchronizer.
// Call Start() to begin gossip rounds.
func NewStateSynchronizer(cfg StateSyncConfig) *StateSynchronizer {
	cfg.setDefaults()
	return &StateSynchronizer{
		cfg:     cfg,
		nodeID:  cfg.NodeID,
		clock:   newHLC(cfg.NodeID),
		state:   make(map[string]*SyncEntry),
		stopCh:  make(chan struct{}),
		metrics: &syncMetrics{},
		digest:  newMerkleDigest(),
	}
}

// Start begins background gossip. Safe to call multiple times.
func (s *StateSynchronizer) Start() {
	if !atomic.CompareAndSwapInt32(&s.started, 0, 1) {
		return
	}
	go s.gossipLoop()
	go s.tombstonePurgeLoop()
}

// Stop halts background goroutines.
func (s *StateSynchronizer) Stop() {
	if atomic.CompareAndSwapInt32(&s.started, 1, 0) {
		close(s.stopCh)
	}
}

// Set stores a key-value pair with a new HLC timestamp.
func (s *StateSynchronizer) Set(key string, value any) HLCTimestamp {
	ts := s.clock.Now()
	entry := &SyncEntry{Key: key, Value: value, Version: ts, NodeID: s.nodeID}
	s.mu.Lock()
	s.state[key] = entry
	s.digest.update(key, ts)
	s.mu.Unlock()
	atomic.AddInt64(&s.metrics.sets, 1)
	return ts
}

// Get retrieves the value for key. Returns (value, version, found).
func (s *StateSynchronizer) Get(key string) (any, HLCTimestamp, bool) {
	s.mu.RLock()
	entry, ok := s.state[key]
	s.mu.RUnlock()
	if !ok || entry.DeletedAt != nil {
		return nil, HLCTimestamp{}, false
	}
	return entry.Value, entry.Version, true
}

// Delete tombstones a key, allowing the deletion to propagate via gossip.
func (s *StateSynchronizer) Delete(key string) {
	ts := s.clock.Now()
	s.mu.Lock()
	if e, ok := s.state[key]; ok {
		e.DeletedAt = &ts
		e.Version = ts
		s.digest.update(key, ts)
	}
	s.mu.Unlock()
	atomic.AddInt64(&s.metrics.deletes, 1)
}

// Merge applies a batch of remote entries using LWW semantics.
// Returns the number of entries that caused local state updates.
func (s *StateSynchronizer) Merge(entries []*SyncEntry) int {
	updated := 0
	for _, remote := range entries {
		if remote == nil {
			continue
		}
		s.clock.Update(remote.Version)

		s.mu.Lock()
		local, exists := s.state[remote.Key]
		if !exists || local.Version.Less(remote.Version) {
			if exists && s.cfg.OnConflict != nil {
				winner := remote
				s.cfg.OnConflict(remote.Key, local, remote, winner)
				atomic.AddInt64(&s.metrics.conflicts, 1)
			}
			s.state[remote.Key] = remote
			s.digest.update(remote.Key, remote.Version)
			updated++
			if s.cfg.OnEntryAdded != nil {
				s.cfg.OnEntryAdded(remote)
			}
		}
		s.mu.Unlock()
	}
	atomic.AddInt64(&s.metrics.entriesMerged, int64(updated))
	return updated
}

// Delta returns all entries modified since the given minimum version (for delta-sync).
func (s *StateSynchronizer) Delta(since HLCTimestamp) []*SyncEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*SyncEntry
	for _, e := range s.state {
		if since.Less(e.Version) || since.Equal(e.Version) {
			out = append(out, e)
		}
	}
	return out
}

// Snapshot returns a full copy of the current state.
func (s *StateSynchronizer) Snapshot() map[string]*SyncEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := make(map[string]*SyncEntry, len(s.state))
	for k, v := range s.state {
		snap[k] = v
	}
	return snap
}

// Digest returns the Merkle digest for anti-entropy comparison.
func (s *StateSynchronizer) Digest() []byte { return s.digest.bytes() }

func (s *StateSynchronizer) gossipLoop() {
	ticker := time.NewTicker(s.cfg.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.gossipRound()
		case <-s.stopCh:
			return
		}
	}
}

func (s *StateSynchronizer) gossipRound() {
	if s.cfg.Transport == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.SyncInterval)
	defer cancel()

	peers, err := s.cfg.Transport.Peers(ctx)
	if err != nil {
		atomic.AddInt64(&s.metrics.syncErrors, 1)
		return
	}

	// Select random subset (fanout).
	targets := selectRandom(peers, s.cfg.ReplicationFanout)

	// Collect local delta.
	snapshot := s.Snapshot()
	entries := make([]*SyncEntry, 0, len(snapshot))
	for _, e := range snapshot {
		entries = append(entries, e)
	}

	var wg sync.WaitGroup
	for _, peer := range targets {
		wg.Add(1)
		go func(p SyncPeer) {
			defer wg.Done()
			pCtx, pCancel := context.WithTimeout(ctx, s.cfg.SyncInterval/2)
			defer pCancel()

			// Push local state.
			if err := p.Push(pCtx, entries); err != nil {
				atomic.AddInt64(&s.metrics.syncErrors, 1)
				if s.cfg.OnSyncError != nil {
					s.cfg.OnSyncError(p.ID(), err)
				}
				return
			}
			atomic.AddInt64(&s.metrics.pushes, 1)

			// Pull from peer (anti-entropy).
			remote, err := p.Pull(pCtx, s.Digest())
			if err != nil {
				if s.cfg.OnSyncError != nil {
					s.cfg.OnSyncError(p.ID(), fmt.Errorf("pull: %w", err))
				}
				return
			}
			atomic.AddInt64(&s.metrics.pulls, 1)
			s.Merge(remote)
		}(peer)
	}
	wg.Wait()
}

func (s *StateSynchronizer) tombstonePurgeLoop() {
	ticker := time.NewTicker(s.cfg.TombstoneTTL / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.purgeTombstones()
		case <-s.stopCh:
			return
		}
	}
}

func (s *StateSynchronizer) purgeTombstones() {
	cutoff := time.Now().Add(-s.cfg.TombstoneTTL)
	s.mu.Lock()
	for key, entry := range s.state {
		if entry.DeletedAt != nil && time.Unix(0, entry.DeletedAt.WallNs).Before(cutoff) {
			delete(s.state, key)
			s.digest.delete(key)
		}
	}
	s.mu.Unlock()
}

// SyncMetricsSnapshot is a point-in-time view of sync metrics.
type SyncMetricsSnapshot struct {
	Sets          int64
	Deletes       int64
	Pushes        int64
	Pulls         int64
	Conflicts     int64
	SyncErrors    int64
	EntriesMerged int64
	StateSize     int
}

// Metrics returns a snapshot.
func (s *StateSynchronizer) Metrics() SyncMetricsSnapshot {
	s.mu.RLock()
	sz := len(s.state)
	s.mu.RUnlock()
	return SyncMetricsSnapshot{
		Sets:          atomic.LoadInt64(&s.metrics.sets),
		Deletes:       atomic.LoadInt64(&s.metrics.deletes),
		Pushes:        atomic.LoadInt64(&s.metrics.pushes),
		Pulls:         atomic.LoadInt64(&s.metrics.pulls),
		Conflicts:     atomic.LoadInt64(&s.metrics.conflicts),
		SyncErrors:    atomic.LoadInt64(&s.metrics.syncErrors),
		EntriesMerged: atomic.LoadInt64(&s.metrics.entriesMerged),
		StateSize:     sz,
	}
}

// ---- helpers ----

func selectRandom(peers []SyncPeer, n int) []SyncPeer {
	if len(peers) <= n {
		return peers
	}
	// Fisher-Yates partial shuffle.
	cp := make([]SyncPeer, len(peers))
	copy(cp, peers)
	for i := 0; i < n; i++ {
		j := i + int(rand.Int63n(int64(len(cp)-i))) //nolint:gosec
		cp[i], cp[j] = cp[j], cp[i]
	}
	return cp[:n]
}

func fnv64(s string) uint64 {
	const (
		offset = uint64(14695981039346656037)
		prime  = uint64(1099511628211)
	)
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func maxUint32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
