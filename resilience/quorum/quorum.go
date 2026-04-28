package flowguard

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// QuorumExecutor executes a call across N backends and returns when a
// configurable quorum (W for writes, R for reads) has responded successfully.
// It implements the classic NWR quorum model used by Dynamo, Cassandra, and Riak.
//
// Features:
//   - Configurable read/write quorums (e.g. W=2, R=2, N=3 for strong consistency)
//   - Parallel fan-out with early return on quorum
//   - Conflict resolution via versioned values (last-write-wins or custom)
//   - Per-node failure tracking and health scoring
//   - Full metrics and observability hooks
//
// Example (Quorum Read):
//
//	q := NewQuorumExecutor(QuorumConfig{
//	    Nodes:        nodes, // []QuorumNode
//	    WriteQuorum:  2,
//	    ReadQuorum:   2,
//	    Timeout:      200 * time.Millisecond,
//	    MergeFunc:    LatestVersionMerge,
//	})
//	result, err := q.Read(ctx, "key:user:42")
type QuorumExecutor struct {
	cfg     QuorumConfig
	metrics *quorumMetrics
	health  []nodeHealth
	mu      sync.RWMutex
}

// QuorumNode represents a backend node the quorum calls fan out to.
type QuorumNode interface {
	// ID returns a stable node identifier (for metrics/logging).
	ID() string

	// Read fetches a VersionedValue for the given key.
	Read(ctx context.Context, key string) (*VersionedValue, error)

	// Write persists a VersionedValue for the given key.
	Write(ctx context.Context, key string, value *VersionedValue) error

	// Delete removes the key from this node.
	Delete(ctx context.Context, key string) error
}

// VersionedValue carries a value with a logical timestamp for conflict resolution.
type VersionedValue struct {
	Value     []byte
	Version   uint64    // Monotonically increasing version (e.g. Lamport clock)
	Timestamp time.Time // Wall clock; used as tiebreaker
	NodeID    string    // Node that produced this version
}

// MergeFunc resolves conflicts when quorum responses differ.
type MergeFunc func(values []*VersionedValue) *VersionedValue

// LatestVersionMerge picks the highest-version value; ties broken by timestamp.
func LatestVersionMerge(values []*VersionedValue) *VersionedValue {
	if len(values) == 0 {
		return nil
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Version != values[j].Version {
			return values[i].Version > values[j].Version
		}
		return values[i].Timestamp.After(values[j].Timestamp)
	})
	return values[0]
}

// QuorumConfig configures the QuorumExecutor.
type QuorumConfig struct {
	// Nodes is the full set of backend nodes (N).
	Nodes []QuorumNode

	// WriteQuorum is the number of nodes that must ACK a write (W).
	WriteQuorum int

	// ReadQuorum is the number of nodes that must respond to a read (R).
	ReadQuorum int

	// Timeout is the per-operation deadline (fan-out + merge).
	Timeout time.Duration

	// MergeFunc resolves conflicting read responses. Default: LatestVersionMerge.
	MergeFunc MergeFunc

	// ReadRepair, if true, asynchronously writes the winning value back to
	// nodes that returned stale data.
	ReadRepair bool

	// HealthScoreDecay controls how quickly node health recovers after errors.
	// Higher = slower decay. Default: 0.9.
	HealthScoreDecay float64

	// OnQuorumFailure is called when quorum cannot be reached.
	OnQuorumFailure func(op string, key string, errors []error)

	// OnReadRepair is called after a read-repair write.
	OnReadRepair func(key string, nodeID string)
}

func (c *QuorumConfig) setDefaults() {
	if c.WriteQuorum == 0 {
		c.WriteQuorum = len(c.Nodes)/2 + 1
	}
	if c.ReadQuorum == 0 {
		c.ReadQuorum = len(c.Nodes)/2 + 1
	}
	if c.Timeout == 0 {
		c.Timeout = 200 * time.Millisecond
	}
	if c.MergeFunc == nil {
		c.MergeFunc = LatestVersionMerge
	}
	if c.HealthScoreDecay == 0 {
		c.HealthScoreDecay = 0.9
	}
}

type nodeHealth struct {
	nodeID string
	score  float64 // 0.0 (dead) – 1.0 (perfect)
	mu     sync.Mutex
}

func (h *nodeHealth) recordSuccess() {
	h.mu.Lock()
	h.score = h.score*0.9 + 0.1 // EWMA toward 1.0
	if h.score > 1.0 {
		h.score = 1.0
	}
	h.mu.Unlock()
}

func (h *nodeHealth) recordFailure(decay float64) {
	h.mu.Lock()
	h.score *= decay
	h.mu.Unlock()
}

func (h *nodeHealth) get() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.score
}

type quorumMetrics struct {
	reads        int64
	writes       int64
	readSuccess  int64
	writeSuccess int64
	readFailure  int64
	writeFailure int64
	readRepairs  int64
	avgReadNodes int64 // sum of nodes that responded (divide by readSuccess)
}

// QuorumReadResult is the result of a quorum read.
type QuorumReadResult struct {
	Value       *VersionedValue
	RespondedN  int  // number of nodes that responded
	ConflictN   int  // number of nodes with conflicting versions
	ReadRepaired bool
}

// NewQuorumExecutor creates a new QuorumExecutor.
func NewQuorumExecutor(cfg QuorumConfig) *QuorumExecutor {
	cfg.setDefaults()
	health := make([]nodeHealth, len(cfg.Nodes))
	for i, n := range cfg.Nodes {
		health[i] = nodeHealth{nodeID: n.ID(), score: 1.0}
	}
	return &QuorumExecutor{cfg: cfg, metrics: &quorumMetrics{}, health: health}
}

// Read executes a quorum read for the given key.
func (q *QuorumExecutor) Read(ctx context.Context, key string) (*QuorumReadResult, error) {
	atomic.AddInt64(&q.metrics.reads, 1)

	ctx, cancel := context.WithTimeout(ctx, q.cfg.Timeout)
	defer cancel()

	type nodeResp struct {
		val    *VersionedValue
		err    error
		nodeID string
		idx    int
	}

	nodes := q.orderedNodes()
	respCh := make(chan nodeResp, len(nodes))

	for i, node := range nodes {
		go func(idx int, n QuorumNode) {
			val, err := n.Read(ctx, key)
			respCh <- nodeResp{val: val, err: err, nodeID: n.ID(), idx: idx}
		}(i, node)
	}

	var (
		successes []*VersionedValue
		errs      []error
		respCount int
	)

	for respCount < len(nodes) {
		select {
		case r := <-respCh:
			respCount++
			if r.err != nil {
				errs = append(errs, fmt.Errorf("node %s: %w", r.nodeID, r.err))
				q.health[r.idx].recordFailure(q.cfg.HealthScoreDecay)
			} else {
				successes = append(successes, r.val)
				q.health[r.idx].recordSuccess()
			}
			if len(successes) >= q.cfg.ReadQuorum {
				cancel()
				goto quorumReached
			}
			if len(errs) > len(nodes)-q.cfg.ReadQuorum {
				// Quorum impossible even if remaining all succeed.
				if q.cfg.OnQuorumFailure != nil {
					q.cfg.OnQuorumFailure("read", key, errs)
				}
				atomic.AddInt64(&q.metrics.readFailure, 1)
				return nil, fmt.Errorf("flowguard: read quorum failed for key %q (%d errors): %w",
					key, len(errs), errs[0])
			}
		case <-ctx.Done():
			atomic.AddInt64(&q.metrics.readFailure, 1)
			return nil, fmt.Errorf("flowguard: read quorum timeout for key %q: %w", key, ctx.Err())
		}
	}

quorumReached:
	atomic.AddInt64(&q.metrics.readSuccess, 1)

	winner := q.cfg.MergeFunc(successes)
	conflictN := countConflicts(successes)

	result := &QuorumReadResult{
		Value:      winner,
		RespondedN: len(successes),
		ConflictN:  conflictN,
	}

	// Read repair: async write winner back to stale nodes.
	if q.cfg.ReadRepair && conflictN > 0 && winner != nil {
		go q.readRepair(key, winner, successes, nodes)
		result.ReadRepaired = true
	}

	return result, nil
}

// Write executes a quorum write for the given key and value.
func (q *QuorumExecutor) Write(ctx context.Context, key string, value *VersionedValue) error {
	atomic.AddInt64(&q.metrics.writes, 1)

	ctx, cancel := context.WithTimeout(ctx, q.cfg.Timeout)
	defer cancel()

	type nodeResp struct {
		err    error
		nodeID string
		idx    int
	}

	nodes := q.orderedNodes()
	respCh := make(chan nodeResp, len(nodes))

	for i, node := range nodes {
		go func(idx int, n QuorumNode) {
			err := n.Write(ctx, key, value)
			respCh <- nodeResp{err: err, nodeID: n.ID(), idx: idx}
		}(i, node)
	}

	var (
		acks     int
		errs     []error
		respCount int
	)

	for respCount < len(nodes) {
		select {
		case r := <-respCh:
			respCount++
			if r.err != nil {
				errs = append(errs, fmt.Errorf("node %s: %w", r.nodeID, r.err))
				q.health[r.idx].recordFailure(q.cfg.HealthScoreDecay)
			} else {
				acks++
				q.health[r.idx].recordSuccess()
			}
			if acks >= q.cfg.WriteQuorum {
				atomic.AddInt64(&q.metrics.writeSuccess, 1)
				return nil
			}
			if len(errs) > len(nodes)-q.cfg.WriteQuorum {
				if q.cfg.OnQuorumFailure != nil {
					q.cfg.OnQuorumFailure("write", key, errs)
				}
				atomic.AddInt64(&q.metrics.writeFailure, 1)
				return fmt.Errorf("flowguard: write quorum failed for key %q (%d acks, need %d): %w",
					key, acks, q.cfg.WriteQuorum, errs[0])
			}
		case <-ctx.Done():
			atomic.AddInt64(&q.metrics.writeFailure, 1)
			return fmt.Errorf("flowguard: write quorum timeout for key %q: %w", key, ctx.Err())
		}
	}

	atomic.AddInt64(&q.metrics.writeFailure, 1)
	return fmt.Errorf("flowguard: write quorum not met for key %q (got %d, need %d)", key, acks, q.cfg.WriteQuorum)
}

// Delete executes a quorum delete for the given key.
func (q *QuorumExecutor) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, q.cfg.Timeout)
	defer cancel()

	type nodeResp struct {
		err    error
		nodeID string
		idx    int
	}

	nodes := q.orderedNodes()
	respCh := make(chan nodeResp, len(nodes))

	for i, node := range nodes {
		go func(idx int, n QuorumNode) {
			err := n.Delete(ctx, key)
			respCh <- nodeResp{err: err, nodeID: n.ID(), idx: idx}
		}(i, node)
	}

	acks, errs := 0, []error{}
	for i := 0; i < len(nodes); i++ {
		select {
		case r := <-respCh:
			if r.err != nil {
				errs = append(errs, r.err)
				q.health[r.idx].recordFailure(q.cfg.HealthScoreDecay)
			} else {
				acks++
				q.health[r.idx].recordSuccess()
			}
			if acks >= q.cfg.WriteQuorum {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("flowguard: delete quorum timeout: %w", ctx.Err())
		}
	}
	return fmt.Errorf("flowguard: delete quorum not met (got %d, need %d, errors: %v)",
		acks, q.cfg.WriteQuorum, errs)
}

// NodeHealth returns a snapshot of per-node health scores.
func (q *QuorumExecutor) NodeHealth() map[string]float64 {
	out := make(map[string]float64, len(q.health))
	for i, h := range q.health {
		out[q.cfg.Nodes[i].ID()] = h.get()
	}
	return out
}

// QuorumMetricsSnapshot is a point-in-time view of quorum metrics.
type QuorumMetricsSnapshot struct {
	Reads        int64
	Writes       int64
	ReadSuccess  int64
	WriteSuccess int64
	ReadFailure  int64
	WriteFailure int64
	ReadRepairs  int64
	ReadSuccessRate float64
	WriteSuccessRate float64
}

// Metrics returns a snapshot of quorum metrics.
func (q *QuorumExecutor) Metrics() QuorumMetricsSnapshot {
	rs := atomic.LoadInt64(&q.metrics.readSuccess)
	rf := atomic.LoadInt64(&q.metrics.readFailure)
	ws := atomic.LoadInt64(&q.metrics.writeSuccess)
	wf := atomic.LoadInt64(&q.metrics.writeFailure)

	rRate, wRate := 0.0, 0.0
	if rs+rf > 0 {
		rRate = float64(rs) / float64(rs+rf)
	}
	if ws+wf > 0 {
		wRate = float64(ws) / float64(ws+wf)
	}
	return QuorumMetricsSnapshot{
		Reads:            atomic.LoadInt64(&q.metrics.reads),
		Writes:           atomic.LoadInt64(&q.metrics.writes),
		ReadSuccess:      rs,
		WriteSuccess:     ws,
		ReadFailure:      rf,
		WriteFailure:     wf,
		ReadRepairs:      atomic.LoadInt64(&q.metrics.readRepairs),
		ReadSuccessRate:  rRate,
		WriteSuccessRate: wRate,
	}
}

// orderedNodes returns nodes sorted by health score descending,
// ensuring healthy nodes are queried first.
func (q *QuorumExecutor) orderedNodes() []QuorumNode {
	type scored struct {
		node  QuorumNode
		score float64
		idx   int
	}
	ss := make([]scored, len(q.cfg.Nodes))
	for i, n := range q.cfg.Nodes {
		ss[i] = scored{node: n, score: q.health[i].get(), idx: i}
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].score > ss[j].score })
	out := make([]QuorumNode, len(ss))
	for i, s := range ss {
		out[i] = s.node
	}
	return out
}

func (q *QuorumExecutor) readRepair(key string, winner *VersionedValue, responses []*VersionedValue, nodes []QuorumNode) {
	winnerVersion := winner.Version
	for i, resp := range responses {
		if resp != nil && resp.Version == winnerVersion {
			continue
		}
		if i >= len(nodes) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), q.cfg.Timeout)
		err := nodes[i].Write(ctx, key, winner)
		cancel()
		if err == nil {
			atomic.AddInt64(&q.metrics.readRepairs, 1)
			if q.cfg.OnReadRepair != nil {
				q.cfg.OnReadRepair(key, nodes[i].ID())
			}
		}
	}
}

func countConflicts(values []*VersionedValue) int {
	if len(values) == 0 {
		return 0
	}
	versions := make(map[uint64]struct{}, len(values))
	for _, v := range values {
		if v != nil {
			versions[v.Version] = struct{}{}
		}
	}
	if len(versions) > 1 {
		return len(values) - 1
	}
	return 0
}
