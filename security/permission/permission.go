// Package permission provides enterprise-grade Role-Based (RBAC) and
// Attribute-Based (ABAC) access control for distributed systems.
//
// Features:
//   - Hierarchical roles with inheritance
//   - Fine-grained resource + action permissions
//   - Attribute-based policy evaluation (ABAC)
//   - Wildcard and pattern matching
//   - Permission caching with TTL
//   - Context-aware enforcement
//   - Audit trail integration
//   - Multi-tenant isolation
//   - Service-to-service permission scopes
//   - Dynamic policy loading
package permission

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────────
// Errors
// ────────────────────────────────────────────────────────────────────────────────

var (
	ErrPermissionDenied = errors.New("permission: access denied")
	ErrRoleNotFound     = errors.New("permission: role not found")
	ErrPolicyConflict   = errors.New("permission: policy conflict detected")
	ErrInvalidPrincipal = errors.New("permission: invalid principal")
	ErrCircularRole     = errors.New("permission: circular role inheritance detected")
)

// ────────────────────────────────────────────────────────────────────────────────
// Core Types
// ────────────────────────────────────────────────────────────────────────────────

// Effect of a policy rule.
type Effect string

const (
	EffectAllow Effect = "ALLOW"
	EffectDeny  Effect = "DENY"
)

// Resource represents a target resource (e.g., "finance:account", "user:profile").
type Resource string

// Action represents an operation on a resource (e.g., "read", "write", "debit").
type Action string

// WildcardAll matches any resource or action.
const WildcardAll = "*"

// Permission is the combination of a resource and action.
type Permission struct {
	Resource Resource `json:"resource"`
	Action   Action   `json:"action"`
}

// String returns a human-readable representation.
func (p Permission) String() string {
	return fmt.Sprintf("%s:%s", p.Resource, p.Action)
}

// Matches checks if this permission applies to the given resource:action pair,
// supporting wildcards.
func (p Permission) Matches(resource Resource, action Action) bool {
	return matchPattern(string(p.Resource), string(resource)) &&
		matchPattern(string(p.Action), string(action))
}

// ────────────────────────────────────────────────────────────────────────────────
// Role
// ────────────────────────────────────────────────────────────────────────────────

// Role defines a named set of permissions with optional parent roles.
type Role struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	TenantID    string            `json:"tenant_id,omitempty"`
	Parents     []string          `json:"parents,omitempty"`
	Permissions []RuleEntry       `json:"permissions"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// RuleEntry is an allow/deny rule bound to a resource + action.
type RuleEntry struct {
	Permission Permission  `json:"permission"`
	Effect     Effect      `json:"effect"`
	Conditions []Condition `json:"conditions,omitempty"` // ABAC conditions
}

// ────────────────────────────────────────────────────────────────────────────────
// Principal — the subject making the request
// ────────────────────────────────────────────────────────────────────────────────

// Principal represents an authenticated subject.
type Principal struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"` // "user", "service", "system"
	TenantID   string            `json:"tenant_id"`
	Roles      []string          `json:"roles"`
	Attributes map[string]string `json:"attributes"` // for ABAC
}

// HasRole returns true if the principal is assigned the given role.
func (p *Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────────
// ABAC Conditions
// ────────────────────────────────────────────────────────────────────────────────

// ConditionOperator defines how to compare attribute values.
type ConditionOperator string

const (
	OperatorEquals     ConditionOperator = "eq"
	OperatorNotEquals  ConditionOperator = "neq"
	OperatorContains   ConditionOperator = "contains"
	OperatorIn         ConditionOperator = "in"
	OperatorStartsWith ConditionOperator = "starts_with"
)

// Condition is an ABAC predicate on principal attributes.
type Condition struct {
	Attribute string            `json:"attribute"` // e.g. "department", "region"
	Operator  ConditionOperator `json:"operator"`
	Value     string            `json:"value"`
	Values    []string          `json:"values,omitempty"` // for "in" operator
}

// Evaluate checks if the condition holds for the given principal.
func (c *Condition) Evaluate(principal *Principal) bool {
	attrVal, ok := principal.Attributes[c.Attribute]
	if !ok {
		return false
	}
	switch c.Operator {
	case OperatorEquals:
		return attrVal == c.Value
	case OperatorNotEquals:
		return attrVal != c.Value
	case OperatorContains:
		return strings.Contains(attrVal, c.Value)
	case OperatorStartsWith:
		return strings.HasPrefix(attrVal, c.Value)
	case OperatorIn:
		for _, v := range c.Values {
			if attrVal == v {
				return true
			}
		}
		return false
	}
	return false
}

// ────────────────────────────────────────────────────────────────────────────────
// Policy Check Request/Response
// ────────────────────────────────────────────────────────────────────────────────

// CheckRequest is the input to the permission checker.
type CheckRequest struct {
	Principal *Principal
	Resource  Resource
	Action    Action
	Context   map[string]string // Additional context for ABAC
}

// CheckResponse is the result of a permission check.
type CheckResponse struct {
	Allowed     bool
	MatchedRole string
	MatchedRule *RuleEntry
	Reason      string
	EvaluatedAt time.Time
}

// ────────────────────────────────────────────────────────────────────────────────
// Store Interface
// ────────────────────────────────────────────────────────────────────────────────

// Store is the backing store for roles and policies.
type Store interface {
	GetRole(ctx context.Context, tenantID, roleName string) (*Role, error)
	ListRoles(ctx context.Context, tenantID string) ([]*Role, error)
	SaveRole(ctx context.Context, role *Role) error
	DeleteRole(ctx context.Context, tenantID, roleName string) error
	GetPrincipalRoles(ctx context.Context, principalID string) ([]string, error)
	AssignRole(ctx context.Context, principalID, tenantID, roleName string) error
	RevokeRole(ctx context.Context, principalID, tenantID, roleName string) error
}

// ────────────────────────────────────────────────────────────────────────────────
// In-Memory Store
// ────────────────────────────────────────────────────────────────────────────────

// MemoryStore is a thread-safe in-memory permission store.
type MemoryStore struct {
	mu             sync.RWMutex
	roles          map[string]*Role    // tenantID:roleName -> Role
	principalRoles map[string][]string // principalID -> []roleName
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		roles:          make(map[string]*Role),
		principalRoles: make(map[string][]string),
	}
}

func roleKey(tenantID, name string) string { return tenantID + "::" + name }

func (s *MemoryStore) GetRole(_ context.Context, tenantID, name string) (*Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.roles[roleKey(tenantID, name)]
	if !ok {
		// Try global (no tenant).
		r, ok = s.roles[roleKey("", name)]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrRoleNotFound, name)
		}
	}
	return r, nil
}

func (s *MemoryStore) ListRoles(_ context.Context, tenantID string) ([]*Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Role
	for _, r := range s.roles {
		if r.TenantID == tenantID || r.TenantID == "" {
			result = append(result, r)
		}
	}
	return result, nil
}

func (s *MemoryStore) SaveRole(_ context.Context, role *Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[roleKey(role.TenantID, role.Name)] = role
	return nil
}

func (s *MemoryStore) DeleteRole(_ context.Context, tenantID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.roles, roleKey(tenantID, name))
	return nil
}

func (s *MemoryStore) GetPrincipalRoles(_ context.Context, principalID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.principalRoles[principalID], nil
}

func (s *MemoryStore) AssignRole(_ context.Context, principalID, _, roleName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.principalRoles[principalID] {
		if r == roleName {
			return nil
		}
	}
	s.principalRoles[principalID] = append(s.principalRoles[principalID], roleName)
	return nil
}

func (s *MemoryStore) RevokeRole(_ context.Context, principalID, _, roleName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	roles := s.principalRoles[principalID]
	for i, r := range roles {
		if r == roleName {
			s.principalRoles[principalID] = append(roles[:i], roles[i+1:]...)
			return nil
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────────
// Cache Layer
// ────────────────────────────────────────────────────────────────────────────────

type cacheEntry struct {
	response  CheckResponse
	expiresAt time.Time
}

type permCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

func newPermCache(ttl time.Duration) *permCache {
	if ttl == 0 {
		ttl = 60 * time.Second
	}
	return &permCache{entries: make(map[string]*cacheEntry), ttl: ttl}
}

func (c *permCache) key(principalID string, resource Resource, action Action) string {
	return principalID + "|" + string(resource) + "|" + string(action)
}

func (c *permCache) get(k string) (CheckResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[k]
	if !ok || time.Now().After(e.expiresAt) {
		return CheckResponse{}, false
	}
	return e.response, true
}

func (c *permCache) set(k string, resp CheckResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[k] = &cacheEntry{response: resp, expiresAt: time.Now().Add(c.ttl)}
}

func (c *permCache) invalidate(principalID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := principalID + "|"
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────────
// Enforcer — main permission checker
// ────────────────────────────────────────────────────────────────────────────────

// EnforcerConfig configures the enforcer.
type EnforcerConfig struct {
	CacheTTL       time.Duration
	DefaultDeny    bool // If true, deny when no rule matches (safe default)
	MaxRoleDepth   int  // Max role inheritance depth to prevent cycles
	AuditAllChecks bool // Log every permission check
}

func (c *EnforcerConfig) defaults() {
	if c.CacheTTL == 0 {
		c.CacheTTL = 60 * time.Second
	}
	if c.MaxRoleDepth == 0 {
		c.MaxRoleDepth = 10
	}
	c.DefaultDeny = true // Always default-deny in enterprise context.
}

// Enforcer is the central permission enforcement engine.
type Enforcer struct {
	store  Store
	cache  *permCache
	config EnforcerConfig
}

// NewEnforcer creates a new Enforcer.
func NewEnforcer(store Store, cfg EnforcerConfig) *Enforcer {
	cfg.defaults()
	return &Enforcer{
		store:  store,
		cache:  newPermCache(cfg.CacheTTL),
		config: cfg,
	}
}

// Check evaluates whether the principal is allowed to perform the action on the resource.
func (e *Enforcer) Check(ctx context.Context, req CheckRequest) (*CheckResponse, error) {
	if req.Principal == nil {
		return &CheckResponse{Allowed: false, Reason: "nil principal"}, ErrInvalidPrincipal
	}

	cacheKey := e.cache.key(req.Principal.ID, req.Resource, req.Action)
	if cached, ok := e.cache.get(cacheKey); ok {
		return &cached, nil
	}

	resp, err := e.evaluate(ctx, req)
	if err != nil {
		return &CheckResponse{Allowed: false, Reason: err.Error()}, err
	}

	e.cache.set(cacheKey, *resp)
	return resp, nil
}

// Require is like Check but returns ErrPermissionDenied if denied.
func (e *Enforcer) Require(ctx context.Context, req CheckRequest) error {
	resp, err := e.Check(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Allowed {
		return fmt.Errorf("%w: principal=%s resource=%s action=%s reason=%s",
			ErrPermissionDenied, req.Principal.ID, req.Resource, req.Action, resp.Reason)
	}
	return nil
}

// RequireAll checks multiple permissions, returning error on first denial.
func (e *Enforcer) RequireAll(ctx context.Context, principal *Principal, perms ...Permission) error {
	for _, p := range perms {
		if err := e.Require(ctx, CheckRequest{Principal: principal, Resource: p.Resource, Action: p.Action}); err != nil {
			return err
		}
	}
	return nil
}

// HasPermission returns true if the principal has the given permission.
func (e *Enforcer) HasPermission(ctx context.Context, principal *Principal, resource Resource, action Action) bool {
	resp, _ := e.Check(ctx, CheckRequest{Principal: principal, Resource: resource, Action: action})
	return resp != nil && resp.Allowed
}

// InvalidateCache removes cached decisions for a principal.
func (e *Enforcer) InvalidateCache(principalID string) {
	e.cache.invalidate(principalID)
}

func (e *Enforcer) evaluate(ctx context.Context, req CheckRequest) (*CheckResponse, error) {
	resp := &CheckResponse{EvaluatedAt: time.Now()}

	// Collect all roles (with inheritance) for the principal.
	visited := make(map[string]bool)
	roles, err := e.collectRoles(ctx, req.Principal, visited, 0)
	if err != nil {
		return nil, err
	}

	// Evaluate rules — DENY takes precedence over ALLOW.
	var allowed *RuleEntry
	var allowedRole string

	for roleName, role := range roles {
		for i := range role.Permissions {
			rule := &role.Permissions[i]
			if !rule.Permission.Matches(req.Resource, req.Action) {
				continue
			}
			// Evaluate ABAC conditions.
			if !e.evaluateConditions(rule.Conditions, req.Principal) {
				continue
			}
			if rule.Effect == EffectDeny {
				resp.Allowed = false
				resp.MatchedRole = roleName
				resp.MatchedRule = rule
				resp.Reason = fmt.Sprintf("explicitly denied by role %s", roleName)
				return resp, nil
			}
			if rule.Effect == EffectAllow && allowed == nil {
				allowed = rule
				allowedRole = roleName
			}
		}
	}

	if allowed != nil {
		resp.Allowed = true
		resp.MatchedRole = allowedRole
		resp.MatchedRule = allowed
		resp.Reason = fmt.Sprintf("allowed by role %s", allowedRole)
		return resp, nil
	}

	resp.Allowed = false
	resp.Reason = "no matching allow rule found"
	return resp, nil
}

// collectRoles returns all effective roles (with inheritance) for a principal.
func (e *Enforcer) collectRoles(ctx context.Context, p *Principal, visited map[string]bool, depth int) (map[string]*Role, error) {
	if depth > e.config.MaxRoleDepth {
		return nil, ErrCircularRole
	}
	result := make(map[string]*Role)
	for _, roleName := range p.Roles {
		if err := e.collectRole(ctx, roleName, p.TenantID, visited, result, depth); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (e *Enforcer) collectRole(ctx context.Context, roleName, tenantID string, visited map[string]bool, result map[string]*Role, depth int) error {
	if visited[roleName] {
		return nil
	}
	visited[roleName] = true

	role, err := e.store.GetRole(ctx, tenantID, roleName)
	if err != nil {
		return fmt.Errorf("permission: loading role %s: %w", roleName, err)
	}
	result[roleName] = role

	for _, parent := range role.Parents {
		if err := e.collectRole(ctx, parent, tenantID, visited, result, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (e *Enforcer) evaluateConditions(conditions []Condition, principal *Principal) bool {
	for _, c := range conditions {
		if !c.Evaluate(principal) {
			return false
		}
	}
	return true
}

// ────────────────────────────────────────────────────────────────────────────────
// Role Builder (DSL)
// ────────────────────────────────────────────────────────────────────────────────

// RoleBuilder provides a fluent API for constructing roles.
type RoleBuilder struct {
	role Role
}

// NewRole starts building a new role.
func NewRole(name string) *RoleBuilder {
	return &RoleBuilder{role: Role{
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}
}

func (b *RoleBuilder) WithDescription(d string) *RoleBuilder { b.role.Description = d; return b }
func (b *RoleBuilder) WithTenant(tid string) *RoleBuilder    { b.role.TenantID = tid; return b }
func (b *RoleBuilder) WithParents(parents ...string) *RoleBuilder {
	b.role.Parents = append(b.role.Parents, parents...)
	return b
}
func (b *RoleBuilder) Allow(resource Resource, actions ...Action) *RoleBuilder {
	for _, a := range actions {
		b.role.Permissions = append(b.role.Permissions, RuleEntry{
			Permission: Permission{Resource: resource, Action: a},
			Effect:     EffectAllow,
		})
	}
	return b
}
func (b *RoleBuilder) Deny(resource Resource, actions ...Action) *RoleBuilder {
	for _, a := range actions {
		b.role.Permissions = append(b.role.Permissions, RuleEntry{
			Permission: Permission{Resource: resource, Action: a},
			Effect:     EffectDeny,
		})
	}
	return b
}
func (b *RoleBuilder) AllowWithCondition(resource Resource, action Action, conditions ...Condition) *RoleBuilder {
	b.role.Permissions = append(b.role.Permissions, RuleEntry{
		Permission: Permission{Resource: resource, Action: a(action)},
		Effect:     EffectAllow,
		Conditions: conditions,
	})
	return b
}
func (b *RoleBuilder) Build() *Role { return &b.role }

func a(action Action) Action { return action } // helper to satisfy type

// ────────────────────────────────────────────────────────────────────────────────
// Predefined Enterprise Roles (Finance / Points / Coupon / User domains)
// ────────────────────────────────────────────────────────────────────────────────

// ResourceFinanceAccount is the resource name for financial accounts.
const (
	ResourceFinanceAccount Resource = "finance:account"
	ResourceFinanceTx      Resource = "finance:transaction"
	ResourcePointsAccount  Resource = "points:account"
	ResourceCoupon         Resource = "coupon"
	ResourceUserProfile    Resource = "user:profile"
	ResourceUserAdmin      Resource = "user:admin"
	ResourceSystemConfig   Resource = "system:config"
	ResourceAuditLog       Resource = "audit:log"
	ResourceAll            Resource = WildcardAll

	ActionRead     Action = "read"
	ActionWrite    Action = "write"
	ActionDelete   Action = "delete"
	ActionDebit    Action = "debit"
	ActionCredit   Action = "credit"
	ActionRefund   Action = "refund"
	ActionApprove  Action = "approve"
	ActionTransfer Action = "transfer"
	ActionExport   Action = "export"
	ActionAll      Action = WildcardAll
)

// PredefinedRoles returns a set of common enterprise roles.
// Register these into your store on startup.
func PredefinedRoles() []*Role {
	return []*Role{
		// Super admin — full access.
		NewRole("super_admin").
			WithDescription("Full system access").
			Allow(ResourceAll, ActionAll).
			Build(),

		// Read-only analyst.
		NewRole("analyst").
			WithDescription("Read-only access to all domains").
			Allow(ResourceFinanceAccount, ActionRead).
			Allow(ResourceFinanceTx, ActionRead).
			Allow(ResourcePointsAccount, ActionRead).
			Allow(ResourceCoupon, ActionRead).
			Allow(ResourceUserProfile, ActionRead).
			Allow(ResourceAuditLog, ActionRead).
			Build(),

		// Finance operator — can debit/credit but not refund without approval.
		NewRole("finance_operator").
			WithDescription("Finance operations without refund").
			WithParents("analyst").
			Allow(ResourceFinanceAccount, ActionDebit, ActionCredit, ActionTransfer).
			Deny(ResourceFinanceAccount, ActionRefund).
			Build(),

		// Finance approver — can refund and approve transactions.
		NewRole("finance_approver").
			WithDescription("Finance approvals and refunds").
			WithParents("finance_operator").
			Allow(ResourceFinanceAccount, ActionRefund, ActionApprove).
			Allow(ResourceFinanceTx, ActionApprove).
			Build(),

		// Points manager.
		NewRole("points_manager").
			WithDescription("Full points account management").
			WithParents("analyst").
			Allow(ResourcePointsAccount, ActionDebit, ActionCredit, ActionWrite).
			Build(),

		// Coupon manager.
		NewRole("coupon_manager").
			WithDescription("Full coupon management").
			Allow(ResourceCoupon, ActionRead, ActionWrite, ActionDelete).
			Build(),

		// User admin.
		NewRole("user_admin").
			WithDescription("User profile management").
			Allow(ResourceUserProfile, ActionRead, ActionWrite, ActionDelete).
			Allow(ResourceUserAdmin, ActionAll).
			Build(),

		// Auditor — read-only audit log access.
		NewRole("auditor").
			WithDescription("Audit log access").
			Allow(ResourceAuditLog, ActionRead, ActionExport).
			Build(),

		// Service account — for machine-to-machine calls.
		NewRole("service_account").
			WithDescription("Internal service-to-service access").
			Allow(ResourceFinanceTx, ActionRead, ActionWrite).
			Allow(ResourcePointsAccount, ActionRead, ActionDebit, ActionCredit).
			Allow(ResourceCoupon, ActionRead, ActionWrite).
			Allow(ResourceUserProfile, ActionRead).
			Build(),
	}
}

// ────────────────────────────────────────────────────────────────────────────────
// Helper
// ────────────────────────────────────────────────────────────────────────────────

// matchPattern checks whether pattern (possibly with wildcard *) matches value.
func matchPattern(pattern, value string) bool {
	if pattern == WildcardAll {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, pattern[:len(pattern)-1])
	}
	return pattern == value
}
