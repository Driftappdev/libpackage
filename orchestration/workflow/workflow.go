// Package workflow provides an enterprise-grade workflow/saga engine.
//
// Features:
//   - DAG-based workflow definition (steps with dependencies)
//   - Sequential, parallel, and conditional execution
//   - Saga pattern with compensation (rollback)
//   - Durable execution (persist state to Redis/DB)
//   - Step retry with backoff
//   - Timeout per step and per workflow
//   - Sub-workflows (nested)
//   - Event-driven triggers
//   - Workflow versioning
//   - Prometheus metrics + audit log
//   - Middleware/hooks (before/after step)
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/driftappdev/plugins/common"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// ============================================================
// CORE TYPES
// ============================================================

// StepStatus is the execution status of a workflow step.
type StepStatus string

const (
	StepStatusPending     StepStatus = "pending"
	StepStatusRunning     StepStatus = "running"
	StepStatusCompleted   StepStatus = "completed"
	StepStatusFailed      StepStatus = "failed"
	StepStatusSkipped     StepStatus = "skipped"
	StepStatusCompensated StepStatus = "compensated"
	StepStatusTimedOut    StepStatus = "timed_out"
)

// WorkflowStatus is the overall workflow status.
type WorkflowStatus string

const (
	WorkflowStatusPending      WorkflowStatus = "pending"
	WorkflowStatusRunning      WorkflowStatus = "running"
	WorkflowStatusCompleted    WorkflowStatus = "completed"
	WorkflowStatusFailed       WorkflowStatus = "failed"
	WorkflowStatusCompensating WorkflowStatus = "compensating"
	WorkflowStatusCompensated  WorkflowStatus = "compensated"
	WorkflowStatusCancelled    WorkflowStatus = "cancelled"
)

// StepContext is the execution context passed to step handlers.
type StepContext struct {
	context.Context
	WorkflowID   string
	WorkflowName string
	StepID       string
	StepName     string
	Attempt      int
	Input        map[string]any // Workflow-level input
	StepResults  map[string]any // Results from previously completed steps
	Logger       common.Logger
	Metadata     map[string]string
}

// Get retrieves a step result by step ID.
func (sc *StepContext) Get(stepID string) (any, bool) {
	if sc.StepResults == nil {
		return nil, false
	}
	v, ok := sc.StepResults[stepID]
	return v, ok
}

// GetString retrieves a string step result.
func (sc *StepContext) GetString(stepID string) string {
	v, ok := sc.Get(stepID)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// StepResult is the output of a step execution.
type StepResult struct {
	Output any
	Error  error
}

// StepHandler is the function executed for a workflow step.
type StepHandler func(ctx *StepContext) (any, error)

// CompensationHandler is the rollback function for a saga step.
type CompensationHandler func(ctx *StepContext, output any) error

// ConditionFunc evaluates whether a step should run.
type ConditionFunc func(ctx *StepContext) bool

// ============================================================
// STEP DEFINITION
// ============================================================

// StepDef defines a single step in a workflow.
type StepDef struct {
	ID           string
	Name         string
	Handler      StepHandler
	Compensation CompensationHandler // For saga pattern
	DependsOn    []string            // Step IDs that must complete before this
	Timeout      time.Duration
	Retry        common.RetryConfig
	Condition    ConditionFunc // If nil or returns true, step runs
	Async        bool          // Run in parallel with other async steps
	Metadata     map[string]string
}

// ============================================================
// WORKFLOW DEFINITION
// ============================================================

// WorkflowDef defines the full workflow structure.
type WorkflowDef struct {
	ID          string
	Name        string
	Version     string
	Description string
	Steps       []*StepDef
	Timeout     time.Duration
	Metadata    map[string]string
}

// ============================================================
// WORKFLOW INSTANCE (runtime state)
// ============================================================

// StepState is the persisted state of a step execution.
type StepState struct {
	StepID      string     `json:"step_id"`
	StepName    string     `json:"step_name"`
	Status      StepStatus `json:"status"`
	Attempt     int        `json:"attempt"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Output      any        `json:"output,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// WorkflowInstance is the persisted runtime state of a workflow execution.
type WorkflowInstance struct {
	ID           string                `json:"id"`
	WorkflowName string                `json:"workflow_name"`
	Version      string                `json:"version"`
	Status       WorkflowStatus        `json:"status"`
	Input        map[string]any        `json:"input"`
	Steps        map[string]*StepState `json:"steps"`
	Output       any                   `json:"output,omitempty"`
	Error        string                `json:"error,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
	StartedAt    *time.Time            `json:"started_at,omitempty"`
	CompletedAt  *time.Time            `json:"completed_at,omitempty"`
	Metadata     map[string]string     `json:"metadata,omitempty"`
	TenantID     string                `json:"tenant_id,omitempty"`
}

// ============================================================
// PERSISTENCE
// ============================================================

// WorkflowStore persists workflow instance state.
type WorkflowStore interface {
	Save(ctx context.Context, instance *WorkflowInstance) error
	Load(ctx context.Context, id string) (*WorkflowInstance, error)
	List(ctx context.Context, filter WorkflowFilter) ([]*WorkflowInstance, error)
	Delete(ctx context.Context, id string) error
}

// WorkflowFilter filters workflow instance queries.
type WorkflowFilter struct {
	WorkflowName string
	Status       WorkflowStatus
	TenantID     string
	Limit        int
}

// RedisWorkflowStore persists workflow state in Redis.
type RedisWorkflowStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisWorkflowStore creates a Redis-backed workflow store.
func NewRedisWorkflowStore(client *redis.Client, ttl time.Duration) *RedisWorkflowStore {
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &RedisWorkflowStore{client: client, prefix: "wf:instance:", ttl: ttl}
}

func (r *RedisWorkflowStore) Save(ctx context.Context, instance *WorkflowInstance) error {
	data, err := json.Marshal(instance)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.prefix+instance.ID, data, r.ttl).Err()
}

func (r *RedisWorkflowStore) Load(ctx context.Context, id string) (*WorkflowInstance, error) {
	data, err := r.client.Get(ctx, r.prefix+id).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("workflow instance %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	var inst WorkflowInstance
	return &inst, json.Unmarshal(data, &inst)
}

func (r *RedisWorkflowStore) List(ctx context.Context, filter WorkflowFilter) ([]*WorkflowInstance, error) {
	keys, err := r.client.Keys(ctx, r.prefix+"*").Result()
	if err != nil {
		return nil, err
	}
	var results []*WorkflowInstance
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}
		var inst WorkflowInstance
		if err := json.Unmarshal(data, &inst); err != nil {
			continue
		}
		if filter.WorkflowName != "" && inst.WorkflowName != filter.WorkflowName {
			continue
		}
		if filter.Status != "" && inst.Status != filter.Status {
			continue
		}
		if filter.TenantID != "" && inst.TenantID != filter.TenantID {
			continue
		}
		results = append(results, &inst)
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}
	return results, nil
}

func (r *RedisWorkflowStore) Delete(ctx context.Context, id string) error {
	return r.client.Del(ctx, r.prefix+id).Err()
}

// MemoryWorkflowStore is an in-memory workflow store for testing.
type MemoryWorkflowStore struct {
	mu        sync.RWMutex
	instances map[string]*WorkflowInstance
}

func NewMemoryWorkflowStore() *MemoryWorkflowStore {
	return &MemoryWorkflowStore{instances: make(map[string]*WorkflowInstance)}
}

func (m *MemoryWorkflowStore) Save(_ context.Context, inst *WorkflowInstance) error {
	m.mu.Lock()
	cp := *inst
	m.instances[inst.ID] = &cp
	m.mu.Unlock()
	return nil
}

func (m *MemoryWorkflowStore) Load(_ context.Context, id string) (*WorkflowInstance, error) {
	m.mu.RLock()
	inst, ok := m.instances[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow instance %q not found", id)
	}
	cp := *inst
	return &cp, nil
}

func (m *MemoryWorkflowStore) List(_ context.Context, filter WorkflowFilter) ([]*WorkflowInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []*WorkflowInstance
	for _, inst := range m.instances {
		if filter.WorkflowName != "" && inst.WorkflowName != filter.WorkflowName {
			continue
		}
		if filter.Status != "" && inst.Status != filter.Status {
			continue
		}
		cp := *inst
		results = append(results, &cp)
	}
	return results, nil
}

func (m *MemoryWorkflowStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.instances, id)
	m.mu.Unlock()
	return nil
}

// ============================================================
// HOOKS
// ============================================================

// Hook is called before/after steps and workflow lifecycle events.
type Hook struct {
	BeforeStep   func(ctx context.Context, instance *WorkflowInstance, step *StepDef)
	AfterStep    func(ctx context.Context, instance *WorkflowInstance, step *StepDef, state *StepState)
	OnComplete   func(ctx context.Context, instance *WorkflowInstance)
	OnFail       func(ctx context.Context, instance *WorkflowInstance, err error)
	OnCompensate func(ctx context.Context, instance *WorkflowInstance)
}

// ============================================================
// ENGINE
// ============================================================

// EngineConfig configures the workflow engine.
type EngineConfig struct {
	Store   WorkflowStore
	Logger  common.Logger
	Metrics common.MetricsRecorder
	Hooks   []Hook
}

// Engine is the workflow execution engine.
type Engine struct {
	store   WorkflowStore
	logger  common.Logger
	metrics common.MetricsRecorder
	hooks   []Hook

	mu        sync.RWMutex
	workflows map[string]*WorkflowDef // registered workflow definitions
}

// NewEngine creates a new workflow engine.
func NewEngine(cfg EngineConfig) *Engine {
	if cfg.Logger == nil {
		cfg.Logger = common.MustNewLogger("info")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = common.NoopMetrics{}
	}
	if cfg.Store == nil {
		cfg.Store = NewMemoryWorkflowStore()
	}
	return &Engine{
		store:     cfg.Store,
		logger:    cfg.Logger,
		metrics:   cfg.Metrics,
		hooks:     cfg.Hooks,
		workflows: make(map[string]*WorkflowDef),
	}
}

// Register registers a workflow definition.
func (e *Engine) Register(def *WorkflowDef) error {
	if err := validateDAG(def); err != nil {
		return fmt.Errorf("invalid workflow DAG %q: %w", def.Name, err)
	}
	e.mu.Lock()
	e.workflows[def.Name] = def
	e.mu.Unlock()
	e.logger.Info("workflow: registered", common.String("name", def.Name), common.String("version", def.Version))
	return nil
}

// StartOptions configures a workflow execution.
type StartOptions struct {
	ID       string
	Input    map[string]any
	Metadata map[string]string
	TenantID string
}

// Start creates and begins executing a workflow.
func (e *Engine) Start(ctx context.Context, name string, opts StartOptions) (*WorkflowInstance, error) {
	e.mu.RLock()
	def, ok := e.workflows[name]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow %q not registered", name)
	}

	id := opts.ID
	if id == "" {
		id = uuid.New().String()
	}

	steps := make(map[string]*StepState, len(def.Steps))
	for _, s := range def.Steps {
		steps[s.ID] = &StepState{StepID: s.ID, StepName: s.Name, Status: StepStatusPending}
	}

	now := time.Now()
	instance := &WorkflowInstance{
		ID:           id,
		WorkflowName: name,
		Version:      def.Version,
		Status:       WorkflowStatusRunning,
		Input:        opts.Input,
		Steps:        steps,
		CreatedAt:    now,
		StartedAt:    &now,
		Metadata:     opts.Metadata,
		TenantID:     opts.TenantID,
	}

	if err := e.store.Save(ctx, instance); err != nil {
		return nil, fmt.Errorf("save workflow instance: %w", err)
	}

	e.metrics.IncrCounter("workflow_started_total", map[string]string{"workflow": name})
	e.logger.Info("workflow: started", common.String("id", id), common.String("name", name))

	// Execute asynchronously
	go e.execute(context.Background(), def, instance)

	return instance, nil
}

// execute runs the workflow DAG.
func (e *Engine) execute(ctx context.Context, def *WorkflowDef, instance *WorkflowInstance) {
	// Apply workflow-level timeout
	if def.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, def.Timeout)
		defer cancel()
	}

	stepResults := make(map[string]any)
	completed := make(map[string]bool)

	// Topological execution respecting DependsOn
	for {
		ready := e.readySteps(def, instance, completed)
		if len(ready) == 0 {
			break
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		anyFailed := false

		for _, step := range ready {
			wg.Add(1)
			go func(s *StepDef) {
				defer wg.Done()

				mu.Lock()
				sr := make(map[string]any, len(stepResults))
				for k, v := range stepResults {
					sr[k] = v
				}
				mu.Unlock()

				output, err := e.runStep(ctx, def, instance, s, sr)

				mu.Lock()
				defer mu.Unlock()
				completed[s.ID] = true
				if err != nil {
					anyFailed = true
					instance.Steps[s.ID].Error = err.Error()
				} else if output != nil {
					stepResults[s.ID] = output
				}
			}(step)
		}
		wg.Wait()

		if anyFailed {
			e.handleFailure(ctx, def, instance, stepResults)
			return
		}

		e.store.Save(ctx, instance) //nolint:errcheck
	}

	// All steps done â€” check for any failures
	for _, state := range instance.Steps {
		if state.Status == StepStatusFailed {
			e.handleFailure(ctx, def, instance, stepResults)
			return
		}
	}

	now := time.Now()
	instance.Status = WorkflowStatusCompleted
	instance.CompletedAt = &now
	e.store.Save(ctx, instance) //nolint:errcheck

	duration := now.Sub(*instance.StartedAt)
	e.metrics.RecordDuration("workflow_duration_seconds", *instance.StartedAt,
		map[string]string{"workflow": def.Name, "status": "completed"})
	e.metrics.IncrCounter("workflow_completed_total", map[string]string{"workflow": def.Name})

	for _, hook := range e.hooks {
		if hook.OnComplete != nil {
			hook.OnComplete(ctx, instance)
		}
	}

	e.logger.Info("workflow: completed",
		common.String("id", instance.ID),
		common.String("name", def.Name),
		common.Duration("duration", duration))
}

// readySteps returns steps whose dependencies are all completed.
func (e *Engine) readySteps(def *WorkflowDef, instance *WorkflowInstance, completed map[string]bool) []*StepDef {
	var ready []*StepDef
	for _, step := range def.Steps {
		state := instance.Steps[step.ID]
		if state.Status != StepStatusPending {
			continue
		}
		depsOK := true
		for _, dep := range step.DependsOn {
			if !completed[dep] {
				depsOK = false
				break
			}
		}
		if depsOK {
			ready = append(ready, step)
		}
	}
	return ready
}

// runStep executes a single step with retry and timeout.
func (e *Engine) runStep(ctx context.Context, def *WorkflowDef, instance *WorkflowInstance, step *StepDef, stepResults map[string]any) (any, error) {
	state := instance.Steps[step.ID]
	state.Status = StepStatusRunning
	now := time.Now()
	state.StartedAt = &now

	for _, hook := range e.hooks {
		if hook.BeforeStep != nil {
			hook.BeforeStep(ctx, instance, step)
		}
	}

	// Apply step timeout
	stepCtx := ctx
	if step.Timeout > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		defer cancel()
	}

	sctx := &StepContext{
		Context:      stepCtx,
		WorkflowID:   instance.ID,
		WorkflowName: instance.WorkflowName,
		StepID:       step.ID,
		StepName:     step.Name,
		Input:        instance.Input,
		StepResults:  stepResults,
		Logger:       e.logger.With(common.String("workflow_id", instance.ID), common.String("step", step.ID)),
		Metadata:     step.Metadata,
	}

	// Check condition
	if step.Condition != nil && !step.Condition(sctx) {
		state.Status = StepStatusSkipped
		e.logger.Debug("workflow: step skipped",
			common.String("workflow_id", instance.ID),
			common.String("step", step.ID))
		return nil, nil
	}

	var output any
	retryCfg := step.Retry
	if retryCfg.MaxAttempts == 0 {
		retryCfg.MaxAttempts = 1
	}

	err := common.Retry(stepCtx, retryCfg, func(ctx context.Context) error {
		state.Attempt++
		sctx.Attempt = state.Attempt
		var err error
		output, err = step.Handler(sctx)
		return err
	})

	completedAt := time.Now()
	state.CompletedAt = &completedAt

	if err != nil {
		state.Status = StepStatusFailed
		state.Error = err.Error()
		e.metrics.IncrCounter("workflow_step_failed_total",
			map[string]string{"workflow": def.Name, "step": step.ID})
		e.logger.Error("workflow: step failed",
			common.String("workflow_id", instance.ID),
			common.String("step", step.ID),
			common.Int("attempts", state.Attempt),
			common.Error(err))
		for _, hook := range e.hooks {
			if hook.AfterStep != nil {
				hook.AfterStep(ctx, instance, step, state)
			}
		}
		return nil, err
	}

	state.Status = StepStatusCompleted
	state.Output = output
	e.metrics.IncrCounter("workflow_step_completed_total",
		map[string]string{"workflow": def.Name, "step": step.ID})

	for _, hook := range e.hooks {
		if hook.AfterStep != nil {
			hook.AfterStep(ctx, instance, step, state)
		}
	}

	e.logger.Debug("workflow: step completed",
		common.String("workflow_id", instance.ID),
		common.String("step", step.ID))
	return output, nil
}

// handleFailure runs saga compensation for completed steps (reverse order).
func (e *Engine) handleFailure(ctx context.Context, def *WorkflowDef, instance *WorkflowInstance, stepResults map[string]any) {
	now := time.Now()
	instance.Status = WorkflowStatusCompensating
	e.store.Save(ctx, instance) //nolint:errcheck

	e.logger.Warn("workflow: compensating", common.String("id", instance.ID))

	// Compensate in reverse order
	for i := len(def.Steps) - 1; i >= 0; i-- {
		step := def.Steps[i]
		if step.Compensation == nil {
			continue
		}
		state := instance.Steps[step.ID]
		if state.Status != StepStatusCompleted {
			continue
		}

		sctx := &StepContext{
			Context:     ctx,
			WorkflowID:  instance.ID,
			StepID:      step.ID,
			StepName:    step.Name,
			Input:       instance.Input,
			StepResults: stepResults,
			Logger:      e.logger.With(common.String("workflow_id", instance.ID), common.String("step", step.ID)),
		}

		e.logger.Info("workflow: compensating step",
			common.String("workflow_id", instance.ID),
			common.String("step", step.ID))

		if err := step.Compensation(sctx, state.Output); err != nil {
			e.logger.Error("workflow: compensation failed",
				common.String("step", step.ID),
				common.Error(err))
		} else {
			state.Status = StepStatusCompensated
		}
	}

	instance.Status = WorkflowStatusCompensated
	instance.CompletedAt = &now
	e.store.Save(ctx, instance) //nolint:errcheck

	e.metrics.IncrCounter("workflow_failed_total", map[string]string{"workflow": def.Name})

	for _, hook := range e.hooks {
		if hook.OnCompensate != nil {
			hook.OnCompensate(ctx, instance)
		}
	}
}

// Get loads a workflow instance by ID.
func (e *Engine) Get(ctx context.Context, id string) (*WorkflowInstance, error) {
	return e.store.Load(ctx, id)
}

// List returns workflow instances matching the filter.
func (e *Engine) List(ctx context.Context, filter WorkflowFilter) ([]*WorkflowInstance, error) {
	return e.store.List(ctx, filter)
}

// Cancel attempts to cancel a running workflow (best-effort).
func (e *Engine) Cancel(ctx context.Context, id string) error {
	instance, err := e.store.Load(ctx, id)
	if err != nil {
		return err
	}
	if instance.Status != WorkflowStatusRunning {
		return fmt.Errorf("workflow %q is not running (status: %s)", id, instance.Status)
	}
	now := time.Now()
	instance.Status = WorkflowStatusCancelled
	instance.CompletedAt = &now
	return e.store.Save(ctx, instance)
}

// ============================================================
// DAG VALIDATION
// ============================================================

func validateDAG(def *WorkflowDef) error {
	ids := make(map[string]bool)
	for _, s := range def.Steps {
		if ids[s.ID] {
			return fmt.Errorf("duplicate step ID %q", s.ID)
		}
		ids[s.ID] = true
	}
	for _, s := range def.Steps {
		for _, dep := range s.DependsOn {
			if !ids[dep] {
				return fmt.Errorf("step %q depends on unknown step %q", s.ID, dep)
			}
		}
	}
	// Cycle detection (DFS)
	visited := make(map[string]int) // 0=unvisited, 1=visiting, 2=done
	var dfs func(id string) error
	adj := make(map[string][]string)
	for _, s := range def.Steps {
		adj[s.ID] = s.DependsOn
	}
	dfs = func(id string) error {
		if visited[id] == 1 {
			return fmt.Errorf("cycle detected at step %q", id)
		}
		if visited[id] == 2 {
			return nil
		}
		visited[id] = 1
		for _, dep := range adj[id] {
			if err := dfs(dep); err != nil {
				return err
			}
		}
		visited[id] = 2
		return nil
	}
	for id := range adj {
		if err := dfs(id); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================
// BUILDER
// ============================================================

// WorkflowBuilder provides a fluent API for building workflow definitions.
type WorkflowBuilder struct {
	def WorkflowDef
}

// NewWorkflow starts building a workflow.
func NewWorkflow(name string) *WorkflowBuilder {
	return &WorkflowBuilder{
		def: WorkflowDef{
			ID:      uuid.New().String(),
			Name:    name,
			Version: "1.0",
		},
	}
}

func (b *WorkflowBuilder) WithVersion(v string) *WorkflowBuilder {
	b.def.Version = v
	return b
}

func (b *WorkflowBuilder) WithDescription(desc string) *WorkflowBuilder {
	b.def.Description = desc
	return b
}

func (b *WorkflowBuilder) WithTimeout(d time.Duration) *WorkflowBuilder {
	b.def.Timeout = d
	return b
}

func (b *WorkflowBuilder) AddStep(step *StepDef) *WorkflowBuilder {
	b.def.Steps = append(b.def.Steps, step)
	return b
}

func (b *WorkflowBuilder) Build() *WorkflowDef {
	def := b.def
	return &def
}

// StepBuilder provides a fluent API for building step definitions.
type StepBuilder struct {
	step StepDef
}

// NewStep starts building a step.
func NewStep(id, name string, handler StepHandler) *StepBuilder {
	return &StepBuilder{
		step: StepDef{
			ID:      id,
			Name:    name,
			Handler: handler,
			Retry:   common.RetryConfig{MaxAttempts: 1},
		},
	}
}

func (s *StepBuilder) DependsOn(ids ...string) *StepBuilder {
	s.step.DependsOn = ids
	return s
}

func (s *StepBuilder) WithTimeout(d time.Duration) *StepBuilder {
	s.step.Timeout = d
	return s
}

func (s *StepBuilder) WithRetry(maxAttempts int, initialDelay time.Duration) *StepBuilder {
	s.step.Retry = common.RetryConfig{
		MaxAttempts:   maxAttempts,
		InitialDelay:  initialDelay,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
	return s
}

func (s *StepBuilder) WithCompensation(c CompensationHandler) *StepBuilder {
	s.step.Compensation = c
	return s
}

func (s *StepBuilder) WithCondition(c ConditionFunc) *StepBuilder {
	s.step.Condition = c
	return s
}

func (s *StepBuilder) Async() *StepBuilder {
	s.step.Async = true
	return s
}

func (s *StepBuilder) WithMetadata(key, value string) *StepBuilder {
	if s.step.Metadata == nil {
		s.step.Metadata = make(map[string]string)
	}
	s.step.Metadata[key] = value
	return s
}

func (s *StepBuilder) Build() *StepDef {
	step := s.step
	return &step
}

