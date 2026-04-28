// Package featureflag provides an enterprise-grade feature flag system.
//
// Features:
//   - Multiple backends: Redis, etcd, in-memory, HTTP remote
//   - Targeting rules: user, tenant, percentage, attribute-based
//   - Gradual rollout with canary support
//   - A/B testing with variant assignment
//   - Real-time flag updates via pub/sub
//   - Local cache with TTL and staleness tolerance
//   - Audit logging
//   - Prometheus metrics
//   - Circuit breaker for backend failures
package featureflag

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/driftappdev/plugins/common"
	"github.com/go-redis/redis/v8"
)

// ============================================================
// CORE TYPES
// ============================================================

// FlagType represents the type of a feature flag value.
type FlagType string

const (
	FlagTypeBool   FlagType = "bool"
	FlagTypeString FlagType = "string"
	FlagTypeInt    FlagType = "int"
	FlagTypeFloat  FlagType = "float"
	FlagTypeJSON   FlagType = "json"
)

// TargetingRuleOp is an operator for targeting rules.
type TargetingRuleOp string

const (
	OpEquals      TargetingRuleOp = "eq"
	OpNotEquals   TargetingRuleOp = "ne"
	OpIn          TargetingRuleOp = "in"
	OpNotIn       TargetingRuleOp = "not_in"
	OpContains    TargetingRuleOp = "contains"
	OpStartsWith  TargetingRuleOp = "starts_with"
	OpGreaterThan TargetingRuleOp = "gt"
	OpLessThan    TargetingRuleOp = "lt"
	OpRegex       TargetingRuleOp = "regex"
)

// TargetingCondition is a single condition in a targeting rule.
type TargetingCondition struct {
	Attribute string          `json:"attribute"` // e.g. "user_id", "country", "plan"
	Operator  TargetingRuleOp `json:"operator"`
	Value     any             `json:"value"`
}

// TargetingRule is an ordered rule that returns a variant when matched.
type TargetingRule struct {
	ID         string               `json:"id"`
	Conditions []TargetingCondition `json:"conditions"` // AND logic
	Variant    string               `json:"variant"`
	Priority   int                  `json:"priority"`
}

// Variant is a named value for a feature flag.
type Variant struct {
	Key         string  `json:"key"`
	Value       any     `json:"value"`
	Weight      float64 `json:"weight"` // 0-100, for percentage rollout
	Description string  `json:"description,omitempty"`
}

// Flag is the full definition of a feature flag.
type Flag struct {
	Key            string          `json:"key"`
	Type           FlagType        `json:"type"`
	Description    string          `json:"description"`
	Enabled        bool            `json:"enabled"`
	DefaultVariant string          `json:"default_variant"`
	Variants       []Variant       `json:"variants"`
	TargetingRules []TargetingRule `json:"targeting_rules"`
	RolloutSalt    string          `json:"rollout_salt"` // For consistent hashing
	Tags           []string        `json:"tags"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	ExpiresAt      *time.Time      `json:"expires_at,omitempty"`
}

// EvaluationContext carries attributes for flag evaluation.
type EvaluationContext struct {
	UserID     string         `json:"user_id"`
	TenantID   string         `json:"tenant_id"`
	SessionID  string         `json:"session_id"`
	Attributes map[string]any `json:"attributes"`
}

// Get returns an attribute value, checking built-ins first.
func (ec *EvaluationContext) Get(key string) (any, bool) {
	switch key {
	case "user_id":
		return ec.UserID, ec.UserID != ""
	case "tenant_id":
		return ec.TenantID, ec.TenantID != ""
	case "session_id":
		return ec.SessionID, ec.SessionID != ""
	}
	if ec.Attributes != nil {
		v, ok := ec.Attributes[key]
		return v, ok
	}
	return nil, false
}

// EvaluationResult is the output of flag evaluation.
type EvaluationResult struct {
	FlagKey     string    `json:"flag_key"`
	VariantKey  string    `json:"variant_key"`
	Value       any       `json:"value"`
	Reason      string    `json:"reason"`
	RuleID      string    `json:"rule_id,omitempty"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	CacheHit    bool      `json:"cache_hit"`
	FlagEnabled bool      `json:"flag_enabled"`
}

// ============================================================
// BACKEND INTERFACE
// ============================================================

// Backend is the storage interface for feature flags.
type Backend interface {
	GetFlag(ctx context.Context, key string) (*Flag, error)
	ListFlags(ctx context.Context) ([]*Flag, error)
	SetFlag(ctx context.Context, flag *Flag) error
	DeleteFlag(ctx context.Context, key string) error
	Watch(ctx context.Context, onChange func(flag *Flag)) error
}

// ============================================================
// REDIS BACKEND
// ============================================================

const redisFlagPrefix = "ff:flag:"
const redisPubSubChannel = "ff:updates"

// RedisBackend stores feature flags in Redis with pub/sub for live updates.
type RedisBackend struct {
	client *redis.Client
	logger common.Logger
}

// NewRedisBackend creates a new Redis-backed feature flag store.
func NewRedisBackend(client *redis.Client, logger common.Logger) *RedisBackend {
	return &RedisBackend{client: client, logger: logger}
}

func (r *RedisBackend) GetFlag(ctx context.Context, key string) (*Flag, error) {
	data, err := r.client.Get(ctx, redisFlagPrefix+key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("flag %q not found", key)
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var flag Flag
	if err := json.Unmarshal(data, &flag); err != nil {
		return nil, fmt.Errorf("unmarshal flag: %w", err)
	}
	return &flag, nil
}

func (r *RedisBackend) ListFlags(ctx context.Context) ([]*Flag, error) {
	keys, err := r.client.Keys(ctx, redisFlagPrefix+"*").Result()
	if err != nil {
		return nil, err
	}
	flags := make([]*Flag, 0, len(keys))
	for _, k := range keys {
		data, err := r.client.Get(ctx, k).Bytes()
		if err != nil {
			continue
		}
		var f Flag
		if err := json.Unmarshal(data, &f); err == nil {
			flags = append(flags, &f)
		}
	}
	return flags, nil
}

func (r *RedisBackend) SetFlag(ctx context.Context, flag *Flag) error {
	flag.UpdatedAt = time.Now()
	data, err := json.Marshal(flag)
	if err != nil {
		return err
	}
	if err := r.client.Set(ctx, redisFlagPrefix+flag.Key, data, 0).Err(); err != nil {
		return err
	}
	// Notify subscribers
	r.client.Publish(ctx, redisPubSubChannel, flag.Key) //nolint:errcheck
	return nil
}

func (r *RedisBackend) DeleteFlag(ctx context.Context, key string) error {
	if err := r.client.Del(ctx, redisFlagPrefix+key).Err(); err != nil {
		return err
	}
	r.client.Publish(ctx, redisPubSubChannel, key) //nolint:errcheck
	return nil
}

func (r *RedisBackend) Watch(ctx context.Context, onChange func(flag *Flag)) error {
	sub := r.client.Subscribe(ctx, redisPubSubChannel)
	go func() {
		defer sub.Close()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				flag, err := r.GetFlag(ctx, msg.Payload)
				if err != nil {
					r.logger.Warn("featureflag: watch get flag failed",
						common.String("key", msg.Payload),
						common.Error(err))
					continue
				}
				onChange(flag)
			}
		}
	}()
	return nil
}

// ============================================================
// IN-MEMORY BACKEND (for testing / standalone)
// ============================================================

// MemoryBackend is an in-memory feature flag backend.
type MemoryBackend struct {
	mu        sync.RWMutex
	flags     map[string]*Flag
	listeners []func(*Flag)
}

// NewMemoryBackend creates a new in-memory backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{flags: make(map[string]*Flag)}
}

func (m *MemoryBackend) GetFlag(_ context.Context, key string) (*Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.flags[key]
	if !ok {
		return nil, fmt.Errorf("flag %q not found", key)
	}
	cp := *f
	return &cp, nil
}

func (m *MemoryBackend) ListFlags(_ context.Context) ([]*Flag, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Flag, 0, len(m.flags))
	for _, f := range m.flags {
		cp := *f
		result = append(result, &cp)
	}
	return result, nil
}

func (m *MemoryBackend) SetFlag(_ context.Context, flag *Flag) error {
	flag.UpdatedAt = time.Now()
	m.mu.Lock()
	cp := *flag
	m.flags[flag.Key] = &cp
	listeners := make([]func(*Flag), len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.Unlock()
	for _, l := range listeners {
		l(flag)
	}
	return nil
}

func (m *MemoryBackend) DeleteFlag(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.flags, key)
	m.mu.Unlock()
	return nil
}

func (m *MemoryBackend) Watch(_ context.Context, onChange func(flag *Flag)) error {
	m.mu.Lock()
	m.listeners = append(m.listeners, onChange)
	m.mu.Unlock()
	return nil
}

// ============================================================
// EVALUATOR
// ============================================================

// Evaluator evaluates feature flags against an evaluation context.
type Evaluator struct {
	cb *common.CircuitBreaker
}

// NewEvaluator creates a new flag evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		cb: common.NewCircuitBreaker(common.CircuitBreakerConfig{
			Name:        "featureflag-evaluator",
			MaxFailures: 5,
			Timeout:     10 * time.Second,
		}),
	}
}

// Evaluate evaluates a flag against the given context.
func (e *Evaluator) Evaluate(flag *Flag, ectx *EvaluationContext) EvaluationResult {
	result := EvaluationResult{
		FlagKey:     flag.Key,
		EvaluatedAt: time.Now(),
		FlagEnabled: flag.Enabled,
	}

	if !flag.Enabled {
		result.Reason = "flag_disabled"
		result.VariantKey = flag.DefaultVariant
		result.Value = e.findVariantValue(flag, flag.DefaultVariant)
		return result
	}

	if flag.ExpiresAt != nil && time.Now().After(*flag.ExpiresAt) {
		result.Reason = "flag_expired"
		result.VariantKey = flag.DefaultVariant
		result.Value = e.findVariantValue(flag, flag.DefaultVariant)
		return result
	}

	// Sort targeting rules by priority (lower number = higher priority)
	rules := make([]TargetingRule, len(flag.TargetingRules))
	copy(rules, flag.TargetingRules)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})

	for _, rule := range rules {
		if e.matchRule(rule, ectx) {
			result.Reason = "targeting_rule"
			result.RuleID = rule.ID
			result.VariantKey = rule.Variant
			result.Value = e.findVariantValue(flag, rule.Variant)
			return result
		}
	}

	// Percentage rollout via consistent hashing
	variant, reason := e.percentageRollout(flag, ectx)
	result.VariantKey = variant
	result.Value = e.findVariantValue(flag, variant)
	result.Reason = reason
	return result
}

func (e *Evaluator) findVariantValue(flag *Flag, variantKey string) any {
	for _, v := range flag.Variants {
		if v.Key == variantKey {
			return v.Value
		}
	}
	return nil
}

func (e *Evaluator) matchRule(rule TargetingRule, ectx *EvaluationContext) bool {
	for _, cond := range rule.Conditions {
		attr, ok := ectx.Get(cond.Attribute)
		if !ok {
			return false
		}
		if !e.matchCondition(cond, attr) {
			return false
		}
	}
	return true
}

func (e *Evaluator) matchCondition(cond TargetingCondition, attr any) bool {
	attrStr := fmt.Sprintf("%v", attr)
	valStr := fmt.Sprintf("%v", cond.Value)

	switch cond.Operator {
	case OpEquals:
		return attrStr == valStr
	case OpNotEquals:
		return attrStr != valStr
	case OpIn:
		vals, ok := cond.Value.([]any)
		if !ok {
			return false
		}
		for _, v := range vals {
			if fmt.Sprintf("%v", v) == attrStr {
				return true
			}
		}
		return false
	case OpNotIn:
		vals, ok := cond.Value.([]any)
		if !ok {
			return true
		}
		for _, v := range vals {
			if fmt.Sprintf("%v", v) == attrStr {
				return false
			}
		}
		return true
	case OpContains:
		return strings.Contains(attrStr, valStr)
	case OpStartsWith:
		return strings.HasPrefix(attrStr, valStr)
	case OpGreaterThan:
		an, err1 := strconv.ParseFloat(attrStr, 64)
		vn, err2 := strconv.ParseFloat(valStr, 64)
		return err1 == nil && err2 == nil && an > vn
	case OpLessThan:
		an, err1 := strconv.ParseFloat(attrStr, 64)
		vn, err2 := strconv.ParseFloat(valStr, 64)
		return err1 == nil && err2 == nil && an < vn
	}
	return false
}

// percentageRollout uses consistent hashing for stable assignment.
func (e *Evaluator) percentageRollout(flag *Flag, ectx *EvaluationContext) (string, string) {
	if len(flag.Variants) == 0 {
		return flag.DefaultVariant, "no_variants"
	}

	// Build hash key from flag salt + user ID
	hashKey := flag.RolloutSalt + ":" + ectx.UserID
	if ectx.UserID == "" {
		hashKey = flag.RolloutSalt + ":" + ectx.SessionID
	}

	h := sha256.Sum256([]byte(hashKey))
	hashInt := new(big.Int).SetBytes(h[:])
	maxInt := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
	// percentage in [0, 100)
	pct := new(big.Float).Mul(
		new(big.Float).Quo(
			new(big.Float).SetInt(hashInt),
			new(big.Float).SetInt(maxInt),
		),
		big.NewFloat(100),
	)
	pctFloat, _ := pct.Float64()

	// Weighted variant selection
	var cumulative float64
	for _, v := range flag.Variants {
		cumulative += v.Weight
		if pctFloat < cumulative {
			return v.Key, "percentage_rollout"
		}
	}

	return flag.DefaultVariant, "default"
}

// ============================================================
// MANAGER (main entry point)
// ============================================================

// Manager is the main feature flag manager â€” use this in your services.
type Manager struct {
	backend   Backend
	evaluator *Evaluator
	logger    common.Logger
	metrics   common.MetricsRecorder
	cb        *common.CircuitBreaker

	// Local cache
	cacheMu  sync.RWMutex
	cache    map[string]*cachedFlag
	cacheTTL time.Duration
}

type cachedFlag struct {
	flag     *Flag
	cachedAt time.Time
}

// ManagerConfig configures the feature flag manager.
type ManagerConfig struct {
	Backend  Backend
	Logger   common.Logger
	Metrics  common.MetricsRecorder
	CacheTTL time.Duration
}

// NewManager creates a new feature flag manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = common.MustNewLogger("info")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = common.NoopMetrics{}
	}

	m := &Manager{
		backend:   cfg.Backend,
		evaluator: NewEvaluator(),
		logger:    cfg.Logger,
		metrics:   cfg.Metrics,
		cache:     make(map[string]*cachedFlag),
		cacheTTL:  cfg.CacheTTL,
		cb: common.NewCircuitBreaker(common.CircuitBreakerConfig{
			Name:        "featureflag-backend",
			MaxFailures: 5,
			Timeout:     30 * time.Second,
		}),
	}
	return m, nil
}

// Start begins watching for flag updates.
func (m *Manager) Start(ctx context.Context) error {
	return m.backend.Watch(ctx, func(flag *Flag) {
		m.cacheMu.Lock()
		m.cache[flag.Key] = &cachedFlag{flag: flag, cachedAt: time.Now()}
		m.cacheMu.Unlock()
		m.logger.Info("featureflag: flag updated", common.String("key", flag.Key))
		m.metrics.IncrCounter("featureflag_updates_total", map[string]string{"flag": flag.Key})
	})
}

func (m *Manager) getFlag(ctx context.Context, key string) (*Flag, bool, error) {
	// Check cache
	m.cacheMu.RLock()
	if cf, ok := m.cache[key]; ok && time.Since(cf.cachedAt) < m.cacheTTL {
		m.cacheMu.RUnlock()
		return cf.flag, true, nil
	}
	m.cacheMu.RUnlock()

	// Fetch from backend
	var flag *Flag
	err := m.cb.Execute(ctx, func(ctx context.Context) error {
		f, err := m.backend.GetFlag(ctx, key)
		if err != nil {
			return err
		}
		flag = f
		return nil
	})
	if err != nil {
		// Return stale cache on circuit open
		m.cacheMu.RLock()
		if cf, ok := m.cache[key]; ok {
			m.cacheMu.RUnlock()
			m.logger.Warn("featureflag: using stale cache",
				common.String("key", key),
				common.Error(err))
			return cf.flag, true, nil
		}
		m.cacheMu.RUnlock()
		return nil, false, err
	}

	// Update cache
	m.cacheMu.Lock()
	m.cache[key] = &cachedFlag{flag: flag, cachedAt: time.Now()}
	m.cacheMu.Unlock()
	return flag, false, nil
}

// Evaluate evaluates a flag and returns the full result.
func (m *Manager) Evaluate(ctx context.Context, key string, ectx *EvaluationContext) (EvaluationResult, error) {
	start := time.Now()
	flag, cacheHit, err := m.getFlag(ctx, key)
	if err != nil {
		m.metrics.IncrCounter("featureflag_errors_total", map[string]string{"flag": key})
		return EvaluationResult{}, fmt.Errorf("get flag %q: %w", key, err)
	}

	result := m.evaluator.Evaluate(flag, ectx)
	result.CacheHit = cacheHit

	m.metrics.RecordDuration("featureflag_eval_duration_seconds", start,
		map[string]string{"flag": key, "variant": result.VariantKey})
	m.metrics.IncrCounter("featureflag_evaluations_total",
		map[string]string{"flag": key, "variant": result.VariantKey, "reason": result.Reason})

	m.logger.Debug("featureflag: evaluated",
		common.String("flag", key),
		common.String("variant", result.VariantKey),
		common.String("reason", result.Reason),
		common.Bool("cache_hit", cacheHit))
	return result, nil
}

// Bool evaluates a boolean flag. Returns defaultVal on error.
func (m *Manager) Bool(ctx context.Context, key string, ectx *EvaluationContext, defaultVal bool) bool {
	result, err := m.Evaluate(ctx, key, ectx)
	if err != nil {
		return defaultVal
	}
	if v, ok := result.Value.(bool); ok {
		return v
	}
	return defaultVal
}

// String evaluates a string flag. Returns defaultVal on error.
func (m *Manager) String(ctx context.Context, key string, ectx *EvaluationContext, defaultVal string) string {
	result, err := m.Evaluate(ctx, key, ectx)
	if err != nil {
		return defaultVal
	}
	if v, ok := result.Value.(string); ok {
		return v
	}
	return defaultVal
}

// Int evaluates an integer flag. Returns defaultVal on error.
func (m *Manager) Int(ctx context.Context, key string, ectx *EvaluationContext, defaultVal int) int {
	result, err := m.Evaluate(ctx, key, ectx)
	if err != nil {
		return defaultVal
	}
	switch v := result.Value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		i, _ := strconv.Atoi(v.String())
		return i
	}
	return defaultVal
}

// JSON evaluates a JSON flag and unmarshals into target.
func (m *Manager) JSON(ctx context.Context, key string, ectx *EvaluationContext, target any) error {
	result, err := m.Evaluate(ctx, key, ectx)
	if err != nil {
		return err
	}
	data, err := json.Marshal(result.Value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// IsEnabled is a convenience method that checks if a flag is enabled for a user.
func (m *Manager) IsEnabled(ctx context.Context, key string, ectx *EvaluationContext) bool {
	return m.Bool(ctx, key, ectx, false)
}

// SetFlag creates or updates a feature flag.
func (m *Manager) SetFlag(ctx context.Context, flag *Flag) error {
	if flag.CreatedAt.IsZero() {
		flag.CreatedAt = time.Now()
	}
	if flag.RolloutSalt == "" {
		flag.RolloutSalt = flag.Key
	}
	return m.backend.SetFlag(ctx, flag)
}

// DeleteFlag removes a feature flag.
func (m *Manager) DeleteFlag(ctx context.Context, key string) error {
	m.cacheMu.Lock()
	delete(m.cache, key)
	m.cacheMu.Unlock()
	return m.backend.DeleteFlag(ctx, key)
}

// ListFlags returns all flags.
func (m *Manager) ListFlags(ctx context.Context) ([]*Flag, error) {
	return m.backend.ListFlags(ctx)
}

// ============================================================
// BUILDER (fluent API for flag creation)
// ============================================================

// FlagBuilder provides a fluent API for constructing flags.
type FlagBuilder struct {
	flag Flag
}

// NewFlag starts building a new feature flag.
func NewFlag(key string) *FlagBuilder {
	return &FlagBuilder{
		flag: Flag{
			Key:         key,
			Type:        FlagTypeBool,
			Enabled:     true,
			CreatedAt:   time.Now(),
			RolloutSalt: key,
		},
	}
}

func (b *FlagBuilder) WithDescription(desc string) *FlagBuilder {
	b.flag.Description = desc
	return b
}

func (b *FlagBuilder) WithType(t FlagType) *FlagBuilder {
	b.flag.Type = t
	return b
}

func (b *FlagBuilder) Disabled() *FlagBuilder {
	b.flag.Enabled = false
	return b
}

func (b *FlagBuilder) WithVariant(key string, value any, weight float64) *FlagBuilder {
	b.flag.Variants = append(b.flag.Variants, Variant{Key: key, Value: value, Weight: weight})
	return b
}

// WithBoolVariants adds on/off variants â€” total weight 100.
func (b *FlagBuilder) WithBoolVariants(onWeight float64) *FlagBuilder {
	b.flag.Type = FlagTypeBool
	b.flag.Variants = []Variant{
		{Key: "on", Value: true, Weight: onWeight},
		{Key: "off", Value: false, Weight: 100 - onWeight},
	}
	b.flag.DefaultVariant = "off"
	return b
}

func (b *FlagBuilder) WithDefaultVariant(key string) *FlagBuilder {
	b.flag.DefaultVariant = key
	return b
}

func (b *FlagBuilder) WithTargetingRule(id, variant string, priority int, conds ...TargetingCondition) *FlagBuilder {
	b.flag.TargetingRules = append(b.flag.TargetingRules, TargetingRule{
		ID:         id,
		Variant:    variant,
		Priority:   priority,
		Conditions: conds,
	})
	return b
}

func (b *FlagBuilder) WithTags(tags ...string) *FlagBuilder {
	b.flag.Tags = append(b.flag.Tags, tags...)
	return b
}

func (b *FlagBuilder) ExpiresAt(t time.Time) *FlagBuilder {
	b.flag.ExpiresAt = &t
	return b
}

// Build returns the constructed flag.
func (b *FlagBuilder) Build() *Flag {
	f := b.flag
	return &f
}

// Cond is a convenience constructor for targeting conditions.
func Cond(attribute string, op TargetingRuleOp, value any) TargetingCondition {
	return TargetingCondition{Attribute: attribute, Operator: op, Value: value}
}

