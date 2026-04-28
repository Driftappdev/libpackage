package healthcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	coretypes "github.com/driftappdev/foundation/core/types"
)

// Checker represents a single health check probe.
type Checker interface {
	Check(context.Context) error
}

// FuncChecker adapts a function to the Checker interface.
type FuncChecker func(context.Context) error

func (f FuncChecker) Check(ctx context.Context) error { return f(ctx) }

type item struct {
	checker Checker
	kind    string
}

// Registry stores liveness/readiness checks.
type Registry struct {
	mu    sync.RWMutex
	items map[string]item
}

func NewRegistry() *Registry { return &Registry{items: map[string]item{}} }

func (r *Registry) AddReadiness(name string, checker Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[name] = item{checker: checker, kind: "readiness"}
}

func (r *Registry) AddLiveness(name string, checker Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[name] = item{checker: checker, kind: "liveness"}
}

// Add registers an arbitrary check kind (compat for legacy wrappers).
func (r *Registry) Add(name string, kind string, checker Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[name] = item{checker: checker, kind: kind}
}

type Result struct {
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	CheckedAt time.Time     `json:"checked_at"`
}

func (r *Registry) check(ctx context.Context, kind string) ([]Result, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make([]Result, 0, len(r.items))
	healthy := true
	for name, it := range r.items {
		if it.kind != kind {
			continue
		}
		start := time.Now()
		err := it.checker.Check(ctx)
		res := Result{
			Name:      name,
			Status:    "up",
			Duration:  time.Since(start),
			CheckedAt: time.Now().UTC(),
		}
		if err != nil {
			res.Status = "down"
			res.Error = err.Error()
			healthy = false
		}
		results = append(results, res)
	}
	return results, healthy
}

// Check returns results in shared core/types format.
func (r *Registry) Check(ctx context.Context, kinds ...string) ([]coretypes.Result, bool) {
	allowed := map[string]struct{}{}
	for _, k := range kinds {
		allowed[k] = struct{}{}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make([]coretypes.Result, 0, len(r.items))
	healthy := true
	for name, it := range r.items {
		if len(allowed) > 0 {
			if _, ok := allowed[it.kind]; !ok {
				continue
			}
		}
		start := time.Now()
		err := it.checker.Check(ctx)
		res := coretypes.Result{
			Name:      name,
			CheckedAt: time.Now().UTC(),
			Duration:  time.Since(start),
			Status:    coretypes.StatusUp,
		}
		if err != nil {
			res.Status = coretypes.StatusDown
			res.Error = err.Error()
			healthy = false
		}
		results = append(results, res)
	}
	return results, healthy
}

func ReadinessHandler(reg *Registry) http.Handler {
	return handler(reg, "readiness")
}

func LivenessHandler(reg *Registry) http.Handler {
	return handler(reg, "liveness")
}

func handler(reg *Registry, kind string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		results, ok := reg.check(r.Context(), kind)
		status := http.StatusOK
		bodyStatus := "up"
		if !ok {
			status = http.StatusServiceUnavailable
			bodyStatus = "down"
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  bodyStatus,
			"results": results,
		})
	})
}


