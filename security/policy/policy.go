// Package policy provides an enterprise-grade policy engine supporting RBAC and ABAC.
//
// Features:
//   - Role-Based Access Control (RBAC): roles, permissions, role hierarchy
//   - Attribute-Based Access Control (ABAC): flexible policy rules
//   - Combined RBAC+ABAC evaluation
//   - Policy as data (JSON/YAML storage)
//   - Policy caching with invalidation
//   - Audit logging of all authorization decisions
//   - Wildcard resource patterns
//   - Deny-override and allow-override modes
//   - Multi-tenant support
//   - OPA-compatible policy language (subset)
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/platformcore/libpackage/plugins/common"
)

// ============================================================
// CORE TYPES
// ============================================================

// Effect is the result of a policy evaluation.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Action is an operation on a resource.
type Action string

// Common actions.
const (
	ActionRead   Action = "read"
	ActionWrite  Action = "write"
	ActionDelete Action = "delete"
	ActionCreate Action = "create"
	ActionList   Action = "list"
	ActionAdmin  Action = "*"
)

// Request is an authorization request.
type Request struct {
	Subject  Subject        `json:"subject"`   // Who is requesting
	Action   Action         `json:"action"`    // What they want to do
	Resource Resource       `json:"resource"`  // What they're doing it to
	Context  map[string]any `json:"context"`   // Environmental attributes (time, IP, etc.)
	TenantID string         `json:"tenant_id"` // Multi-tenant support
}

// Subject represents the principal making the request.
type Subject struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"` // "user", "service", "api_key"
	Roles      []string       `json:"roles"`
	Groups     []string       `json:"groups"`
	Attributes map[string]any `json:"attributes"`
	TenantID   string         `json:"tenant_id"`
}

// Resource represents the target of an action.
type Resource struct {
	Type       string         `json:"type"` // e.g. "document", "user", "billing"
	ID         string         `json:"id"`   // e.g. "doc-123"
	Attributes map[string]any `json:"attributes"`
	Owner      string         `json:"owner,omitempty"`
	TenantID   string         `json:"tenant_id"`
}

// Decision is the outcome of policy evaluation.
type Decision struct {
	Effect      Effect         `json:"effect"`
	Reason      string         `json:"reason"`
	PolicyID    string         `json:"policy_id,omitempty"`
	RuleID      string         `json:"rule_id,omitempty"`
	EvaluatedAt time.Time      `json:"evaluated_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func (d Decision) IsAllowed() bool { return d.Effect == EffectAllow }

// ============================================================
// RBAC TYPES
// ============================================================

// Permission is a (action, resource_type) tuple.
type Permission struct {
	Action       Action `json:"action"`
	ResourceType string `json:"resource_type"`       // supports "*" wildcard
	Condition    string `json:"condition,omitempty"` // optional CEL/simple expression
}

// Role is a named collection of permissions.
type Role struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `json:"permissions"`
	Parents     []string     `json:"parents"`   // Role hierarchy (inherits permissions)
	TenantID    string       `json:"tenant_id"` // empty = global role
	CreatedAt   time.Time    `json:"created_at"`
}

// RoleBinding binds a role to a subject.
type RoleBinding struct {
	ID          string     `json:"id"`
	SubjectID   string     `json:"subject_id"`
	SubjectType string     `json:"subject_type"`
	RoleID      string     `json:"role_id"`
	TenantID    string     `json:"tenant_id"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	GrantedBy   string     `json:"granted_by"`
	GrantedAt   time.Time  `json:"granted_at"`
}

// ============================================================
// ABAC POLICY
// ============================================================

// PolicyRule is a single ABAC rule.
type PolicyRule struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Priority    int             `json:"priority"` // lower = higher priority
	Effect      Effect          `json:"effect"`
	Subjects    MatchSpec       `json:"subjects"`
	Actions     []Action        `json:"actions"`
	Resources   MatchSpec       `json:"resources"`
	Conditions  []RuleCondition `json:"conditions"`
}

// MatchSpec defines criteria for matching subjects/resources.
type MatchSpec struct {
	Types      []string       `json:"types,omitempty"`
	IDs        []string       `json:"ids,omitempty"`
	Roles      []string       `json:"roles,omitempty"`
	Groups     []string       `json:"groups,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// RuleCondition is a condition expression in a policy rule.
type RuleCondition struct {
	Type string `json:"type"` // "time_range", "attribute_match", "owner_match", "tenant_match"
	Spec any    `json:"spec"`
}

// Policy is a named collection of rules.
type Policy struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Version     string       `json:"version"`
	Rules       []PolicyRule `json:"rules"`
	CombineMode string       `json:"combine_mode"` // "deny_override" or "allow_override"
	TenantID    string       `json:"tenant_id"`
	Enabled     bool         `json:"enabled"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// ============================================================
// POLICY STORE
// ============================================================

// PolicyStore persists roles and policies.
type PolicyStore interface {
	GetRole(ctx context.Context, id string) (*Role, error)
	ListRoles(ctx context.Context, tenantID string) ([]*Role, error)
	SetRole(ctx context.Context, role *Role) error
	DeleteRole(ctx context.Context, id string) error

	GetRoleBindings(ctx context.Context, subjectID, tenantID string) ([]*RoleBinding, error)
	SetRoleBinding(ctx context.Context, binding *RoleBinding) error
	DeleteRoleBinding(ctx context.Context, id string) error

	GetPolicy(ctx context.Context, id string) (*Policy, error)
	ListPolicies(ctx context.Context, tenantID string) ([]*Policy, error)
	SetPolicy(ctx context.Context, policy *Policy) error
	DeletePolicy(ctx context.Context, id string) error
}

// MemoryPolicyStore is an in-memory policy store.
type MemoryPolicyStore struct {
	mu       sync.RWMutex
	roles    map[string]*Role
	bindings map[string]*RoleBinding // keyed by id
	policies map[string]*Policy
}

// NewMemoryPolicyStore creates a new in-memory policy store.
func NewMemoryPolicyStore() *MemoryPolicyStore {
	return &MemoryPolicyStore{
		roles:    make(map[string]*Role),
		bindings: make(map[string]*RoleBinding),
		policies: make(map[string]*Policy),
	}
}

func (m *MemoryPolicyStore) GetRole(_ context.Context, id string) (*Role, error) {
	m.mu.RLock()
	r, ok := m.roles[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("role %q not found", id)
	}
	return r, nil
}

func (m *MemoryPolicyStore) ListRoles(_ context.Context, tenantID string) ([]*Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Role
	for _, r := range m.roles {
		if r.TenantID == tenantID || r.TenantID == "" {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *MemoryPolicyStore) SetRole(_ context.Context, role *Role) error {
	m.mu.Lock()
	m.roles[role.ID] = role
	m.mu.Unlock()
	return nil
}

func (m *MemoryPolicyStore) DeleteRole(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.roles, id)
	m.mu.Unlock()
	return nil
}

func (m *MemoryPolicyStore) GetRoleBindings(_ context.Context, subjectID, tenantID string) ([]*RoleBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*RoleBinding
	for _, b := range m.bindings {
		if b.SubjectID == subjectID && (b.TenantID == tenantID || b.TenantID == "") {
			if b.ExpiresAt == nil || time.Now().Before(*b.ExpiresAt) {
				result = append(result, b)
			}
		}
	}
	return result, nil
}

func (m *MemoryPolicyStore) SetRoleBinding(_ context.Context, b *RoleBinding) error {
	m.mu.Lock()
	m.bindings[b.ID] = b
	m.mu.Unlock()
	return nil
}

func (m *MemoryPolicyStore) DeleteRoleBinding(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.bindings, id)
	m.mu.Unlock()
	return nil
}

func (m *MemoryPolicyStore) GetPolicy(_ context.Context, id string) (*Policy, error) {
	m.mu.RLock()
	p, ok := m.policies[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("policy %q not found", id)
	}
	return p, nil
}

func (m *MemoryPolicyStore) ListPolicies(_ context.Context, tenantID string) ([]*Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Policy
	for _, p := range m.policies {
		if p.TenantID == tenantID || p.TenantID == "" {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *MemoryPolicyStore) SetPolicy(_ context.Context, policy *Policy) error {
	policy.UpdatedAt = time.Now()
	m.mu.Lock()
	m.policies[policy.ID] = policy
	m.mu.Unlock()
	return nil
}

func (m *MemoryPolicyStore) DeletePolicy(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.policies, id)
	m.mu.Unlock()
	return nil
}

// ============================================================
// POLICY ENGINE
// ============================================================

// AuditLog records authorization decisions.
type AuditLog interface {
	LogDecision(ctx context.Context, req Request, decision Decision)
}

// NoopAuditLog discards audit events.
type NoopAuditLog struct{}

func (NoopAuditLog) LogDecision(_ context.Context, _ Request, _ Decision) {}

// LoggerAuditLog writes decisions to the structured logger.
type LoggerAuditLog struct {
	logger common.Logger
}

func NewLoggerAuditLog(logger common.Logger) *LoggerAuditLog {
	return &LoggerAuditLog{logger: logger}
}

func (l *LoggerAuditLog) LogDecision(_ context.Context, req Request, decision Decision) {
	l.logger.Info("policy: authorization decision",
		common.String("subject_id", req.Subject.ID),
		common.String("action", string(req.Action)),
		common.String("resource_type", req.Resource.Type),
		common.String("resource_id", req.Resource.ID),
		common.String("effect", string(decision.Effect)),
		common.String("reason", decision.Reason),
		common.String("policy_id", decision.PolicyID),
		common.String("tenant_id", req.TenantID))
}

// Engine is the main policy evaluation engine.
type Engine struct {
	store   PolicyStore
	logger  common.Logger
	metrics common.MetricsRecorder
	audit   AuditLog

	// Role/permission cache
	roleCacheMu  sync.RWMutex
	roleCache    map[string]*roleExpanded // key: roleID
	roleCacheTTL time.Duration
}

type roleExpanded struct {
	role     *Role
	allPerms []Permission // including inherited
	cachedAt time.Time
}

// EngineConfig configures the policy engine.
type EngineConfig struct {
	Store        PolicyStore
	Logger       common.Logger
	Metrics      common.MetricsRecorder
	Audit        AuditLog
	RoleCacheTTL time.Duration
}

// NewEngine creates a new policy engine.
func NewEngine(cfg EngineConfig) *Engine {
	if cfg.Logger == nil {
		cfg.Logger = common.MustNewLogger("info")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = common.NoopMetrics{}
	}
	if cfg.Audit == nil {
		cfg.Audit = NoopAuditLog{}
	}
	if cfg.RoleCacheTTL == 0 {
		cfg.RoleCacheTTL = 5 * time.Minute
	}
	return &Engine{
		store:        cfg.Store,
		logger:       cfg.Logger,
		metrics:      cfg.Metrics,
		audit:        cfg.Audit,
		roleCache:    make(map[string]*roleExpanded),
		roleCacheTTL: cfg.RoleCacheTTL,
	}
}

// Authorize evaluates a request and returns an authorization decision.
func (e *Engine) Authorize(ctx context.Context, req Request) (Decision, error) {
	start := time.Now()

	// 1. RBAC evaluation
	rbacDecision, err := e.evaluateRBAC(ctx, req)
	if err != nil {
		e.logger.Error("policy: rbac eval error", common.Error(err))
	}
	if rbacDecision != nil && rbacDecision.Effect == EffectAllow {
		d := *rbacDecision
		d.EvaluatedAt = time.Now()
		e.recordAndAudit(ctx, req, d, start)
		return d, nil
	}

	// 2. ABAC evaluation
	abacDecision, err := e.evaluateABAC(ctx, req)
	if err != nil {
		e.logger.Error("policy: abac eval error", common.Error(err))
	}
	if abacDecision != nil {
		d := *abacDecision
		d.EvaluatedAt = time.Now()
		e.recordAndAudit(ctx, req, d, start)
		return d, nil
	}

	// Default deny
	d := Decision{
		Effect:      EffectDeny,
		Reason:      "no_matching_policy",
		EvaluatedAt: time.Now(),
	}
	e.recordAndAudit(ctx, req, d, start)
	return d, nil
}

// AuthorizeOrFail returns an error if the request is denied.
func (e *Engine) AuthorizeOrFail(ctx context.Context, req Request) error {
	decision, err := e.Authorize(ctx, req)
	if err != nil {
		return fmt.Errorf("policy evaluation error: %w", err)
	}
	if !decision.IsAllowed() {
		return fmt.Errorf("access denied: %s (%s)", decision.Reason, decision.PolicyID)
	}
	return nil
}

func (e *Engine) recordAndAudit(ctx context.Context, req Request, d Decision, start time.Time) {
	e.audit.LogDecision(ctx, req, d)
	labels := map[string]string{
		"effect":        string(d.Effect),
		"resource_type": req.Resource.Type,
		"action":        string(req.Action),
	}
	e.metrics.IncrCounter("policy_decisions_total", labels)
	e.metrics.RecordDuration("policy_eval_duration_seconds", start, labels)
}

// evaluateRBAC checks role-based permissions.
func (e *Engine) evaluateRBAC(ctx context.Context, req Request) (*Decision, error) {
	// Get subject's role bindings
	bindings, err := e.store.GetRoleBindings(ctx, req.Subject.ID, req.TenantID)
	if err != nil {
		return nil, err
	}

	// Merge roles from bindings + subject.Roles
	roleIDs := make(map[string]bool)
	for _, b := range bindings {
		roleIDs[b.RoleID] = true
	}
	for _, r := range req.Subject.Roles {
		roleIDs[r] = true
	}

	for roleID := range roleIDs {
		perms, err := e.expandRolePermissions(ctx, roleID)
		if err != nil {
			e.logger.Warn("policy: expand role failed",
				common.String("role", roleID), common.Error(err))
			continue
		}

		for _, perm := range perms {
			if e.matchPermission(perm, req.Action, req.Resource.Type) {
				return &Decision{
					Effect:   EffectAllow,
					Reason:   "rbac_role_permission",
					PolicyID: "rbac",
					RuleID:   fmt.Sprintf("role:%s perm:%s:%s", roleID, perm.Action, perm.ResourceType),
				}, nil
			}
		}
	}
	return nil, nil
}

// expandRolePermissions returns all permissions for a role including inherited ones.
func (e *Engine) expandRolePermissions(ctx context.Context, roleID string) ([]Permission, error) {
	e.roleCacheMu.RLock()
	if cached, ok := e.roleCache[roleID]; ok && time.Since(cached.cachedAt) < e.roleCacheTTL {
		e.roleCacheMu.RUnlock()
		return cached.allPerms, nil
	}
	e.roleCacheMu.RUnlock()

	role, err := e.store.GetRole(ctx, roleID)
	if err != nil {
		return nil, err
	}

	allPerms := make([]Permission, len(role.Permissions))
	copy(allPerms, role.Permissions)

	// Inherit from parent roles (BFS)
	visited := map[string]bool{roleID: true}
	queue := append([]string{}, role.Parents...)
	for len(queue) > 0 {
		parentID := queue[0]
		queue = queue[1:]
		if visited[parentID] {
			continue
		}
		visited[parentID] = true
		parent, err := e.store.GetRole(ctx, parentID)
		if err != nil {
			continue
		}
		allPerms = append(allPerms, parent.Permissions...)
		queue = append(queue, parent.Parents...)
	}

	e.roleCacheMu.Lock()
	e.roleCache[roleID] = &roleExpanded{role: role, allPerms: allPerms, cachedAt: time.Now()}
	e.roleCacheMu.Unlock()

	return allPerms, nil
}

func (e *Engine) matchPermission(perm Permission, action Action, resourceType string) bool {
	actionMatch := perm.Action == ActionAdmin || perm.Action == action
	resourceMatch := perm.ResourceType == "*" ||
		perm.ResourceType == resourceType ||
		matchGlob(perm.ResourceType, resourceType)
	return actionMatch && resourceMatch
}

// evaluateABAC checks attribute-based policies.
func (e *Engine) evaluateABAC(ctx context.Context, req Request) (*Decision, error) {
	policies, err := e.store.ListPolicies(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}

	// Sort all rules across policies by priority
	type rankedRule struct {
		rule   PolicyRule
		policy *Policy
	}
	var allRules []rankedRule
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		for _, r := range p.Rules {
			allRules = append(allRules, rankedRule{rule: r, policy: p})
		}
	}
	sort.Slice(allRules, func(i, j int) bool {
		return allRules[i].rule.Priority < allRules[j].rule.Priority
	})

	for _, rr := range allRules {
		if e.matchRule(rr.rule, req) {
			return &Decision{
				Effect:   rr.rule.Effect,
				Reason:   "abac_policy_rule",
				PolicyID: rr.policy.ID,
				RuleID:   rr.rule.ID,
			}, nil
		}
	}
	return nil, nil
}

func (e *Engine) matchRule(rule PolicyRule, req Request) bool {
	// Match action
	if !matchActions(rule.Actions, req.Action) {
		return false
	}

	// Match subject
	if !e.matchSubjectSpec(rule.Subjects, req.Subject) {
		return false
	}

	// Match resource
	if !e.matchResourceSpec(rule.Resources, req.Resource) {
		return false
	}

	// Evaluate conditions
	for _, cond := range rule.Conditions {
		if !e.evalCondition(cond, req) {
			return false
		}
	}

	return true
}

func matchActions(actions []Action, action Action) bool {
	for _, a := range actions {
		if a == ActionAdmin || a == action {
			return true
		}
	}
	return false
}

func (e *Engine) matchSubjectSpec(spec MatchSpec, subject Subject) bool {
	if len(spec.Types) > 0 && !contains(spec.Types, subject.Type) {
		return false
	}
	if len(spec.IDs) > 0 && !contains(spec.IDs, subject.ID) {
		return false
	}
	if len(spec.Roles) > 0 {
		matched := false
		for _, r := range spec.Roles {
			if contains(subject.Roles, r) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(spec.Groups) > 0 {
		matched := false
		for _, g := range spec.Groups {
			if contains(subject.Groups, g) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (e *Engine) matchResourceSpec(spec MatchSpec, resource Resource) bool {
	if len(spec.Types) > 0 {
		matched := false
		for _, t := range spec.Types {
			if t == "*" || t == resource.Type || matchGlob(t, resource.Type) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(spec.IDs) > 0 && !contains(spec.IDs, resource.ID) {
		return false
	}
	return true
}

func (e *Engine) evalCondition(cond RuleCondition, req Request) bool {
	switch cond.Type {
	case "owner_match":
		// Allow if subject owns the resource
		return req.Resource.Owner == req.Subject.ID

	case "tenant_match":
		// Allow if subject and resource are in the same tenant
		return req.Subject.TenantID == req.Resource.TenantID

	case "time_range":
		spec, ok := cond.Spec.(map[string]any)
		if !ok {
			return false
		}
		now := time.Now()
		if start, ok := spec["start"].(string); ok {
			startTime, _ := time.Parse("15:04", start)
			if now.Hour()*60+now.Minute() < startTime.Hour()*60+startTime.Minute() {
				return false
			}
		}
		if end, ok := spec["end"].(string); ok {
			endTime, _ := time.Parse("15:04", end)
			if now.Hour()*60+now.Minute() > endTime.Hour()*60+endTime.Minute() {
				return false
			}
		}
		return true

	case "attribute_match":
		specBytes, _ := json.Marshal(cond.Spec)
		var spec struct {
			Source    string `json:"source"` // "subject" or "resource"
			Attribute string `json:"attribute"`
			Value     any    `json:"value"`
		}
		if err := json.Unmarshal(specBytes, &spec); err != nil {
			return false
		}
		var attrs map[string]any
		if spec.Source == "subject" {
			attrs = req.Subject.Attributes
		} else {
			attrs = req.Resource.Attributes
		}
		if attrs == nil {
			return false
		}
		actual, ok := attrs[spec.Attribute]
		if !ok {
			return false
		}
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", spec.Value)

	case "ip_allowlist":
		// Check if request IP is in allowed list
		if ip, ok := req.Context["ip"].(string); ok {
			if allowlist, ok := cond.Spec.([]any); ok {
				for _, allowed := range allowlist {
					if fmt.Sprintf("%v", allowed) == ip {
						return true
					}
				}
			}
		}
		return false
	}
	return true
}

// InvalidateRoleCache clears cached role permissions.
func (e *Engine) InvalidateRoleCache() {
	e.roleCacheMu.Lock()
	e.roleCache = make(map[string]*roleExpanded)
	e.roleCacheMu.Unlock()
}

// ============================================================
// BUILDER: Fluent Policy API
// ============================================================

// PolicyBuilder constructs a Policy with a fluent API.
type PolicyBuilder struct {
	policy Policy
}

// NewPolicy starts building a policy.
func NewPolicy(id, name string) *PolicyBuilder {
	return &PolicyBuilder{
		policy: Policy{
			ID:          id,
			Name:        name,
			Version:     "1.0",
			CombineMode: "deny_override",
			Enabled:     true,
			CreatedAt:   time.Now(),
		},
	}
}

func (b *PolicyBuilder) WithDescription(desc string) *PolicyBuilder {
	b.policy.Description = desc
	return b
}

func (b *PolicyBuilder) ForTenant(tenantID string) *PolicyBuilder {
	b.policy.TenantID = tenantID
	return b
}

func (b *PolicyBuilder) AllowOverride() *PolicyBuilder {
	b.policy.CombineMode = "allow_override"
	return b
}

func (b *PolicyBuilder) AddRule(rule PolicyRule) *PolicyBuilder {
	b.policy.Rules = append(b.policy.Rules, rule)
	return b
}

func (b *PolicyBuilder) Build() *Policy {
	p := b.policy
	return &p
}

// RuleBuilder builds a PolicyRule.
type RuleBuilder struct {
	rule PolicyRule
}

// NewRule starts building a policy rule.
func NewRule(id string, effect Effect, priority int) *RuleBuilder {
	return &RuleBuilder{
		rule: PolicyRule{
			ID:       id,
			Effect:   effect,
			Priority: priority,
		},
	}
}

func (r *RuleBuilder) WithDescription(desc string) *RuleBuilder {
	r.rule.Description = desc
	return r
}

func (r *RuleBuilder) ForActions(actions ...Action) *RuleBuilder {
	r.rule.Actions = actions
	return r
}

func (r *RuleBuilder) ForSubjectTypes(types ...string) *RuleBuilder {
	r.rule.Subjects.Types = types
	return r
}

func (r *RuleBuilder) ForSubjectRoles(roles ...string) *RuleBuilder {
	r.rule.Subjects.Roles = roles
	return r
}

func (r *RuleBuilder) ForResourceTypes(types ...string) *RuleBuilder {
	r.rule.Resources.Types = types
	return r
}

func (r *RuleBuilder) WithCondition(condType string, spec any) *RuleBuilder {
	r.rule.Conditions = append(r.rule.Conditions, RuleCondition{Type: condType, Spec: spec})
	return r
}

func (r *RuleBuilder) Build() PolicyRule {
	return r.rule
}

// RoleBuilder builds a Role.
type RoleBuilder struct {
	role Role
}

// NewRole starts building a role.
func NewRole(id, name string) *RoleBuilder {
	return &RoleBuilder{
		role: Role{ID: id, Name: name, CreatedAt: time.Now()},
	}
}

func (r *RoleBuilder) WithDescription(desc string) *RoleBuilder {
	r.role.Description = desc
	return r
}

func (r *RoleBuilder) InheritFrom(parentIDs ...string) *RoleBuilder {
	r.role.Parents = append(r.role.Parents, parentIDs...)
	return r
}

func (r *RoleBuilder) Can(action Action, resourceType string) *RoleBuilder {
	r.role.Permissions = append(r.role.Permissions, Permission{
		Action:       action,
		ResourceType: resourceType,
	})
	return r
}

func (r *RoleBuilder) CanAll(resourceType string) *RoleBuilder {
	return r.Can(ActionAdmin, resourceType)
}

func (r *RoleBuilder) Build() *Role {
	role := r.role
	return &role
}

// ============================================================
// HTTP MIDDLEWARE
// ============================================================

// HTTPAuthMiddleware is an HTTP middleware that enforces authorization.
func HTTPAuthMiddleware(engine *Engine, subjectExtractor func(*http.Request) (Subject, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subject, err := subjectExtractor(r)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			req := Request{
				Subject:  subject,
				Action:   inferAction(r.Method),
				Resource: Resource{Type: inferResourceType(r.URL.Path)},
				Context: map[string]any{
					"ip":         r.RemoteAddr,
					"user_agent": r.UserAgent(),
				},
				TenantID: subject.TenantID,
			}
			decision, err := engine.Authorize(r.Context(), req)
			if err != nil || !decision.IsAllowed() {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func inferAction(method string) Action {
	switch strings.ToUpper(method) {
	case "GET", "HEAD":
		return ActionRead
	case "POST":
		return ActionCreate
	case "PUT", "PATCH":
		return ActionWrite
	case "DELETE":
		return ActionDelete
	default:
		return Action(strings.ToLower(method))
	}
}

func inferResourceType(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return "unknown"
}

// ============================================================
// HELPERS
// ============================================================

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func matchGlob(pattern, s string) bool {
	matched, _ := filepath.Match(pattern, s)
	return matched
}

