package registry

import (
	"errors"
	"sync"

	"github.com/platformcore/libpackage/orchestration/di/provider"
)

type Entry struct {
	Name string
	Tags []string
}

type Registry struct {
	mu            sync.RWMutex
	entries       []Entry
	providerItems map[string]provider.Definition
}

func New() *Registry {
	return &Registry{providerItems: map[string]provider.Definition{}}
}

func (r *Registry) Add(entry Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
}
func (r *Registry) Entries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

func (r *Registry) Register(def provider.Definition) error {
	if def.Name == "" {
		return errors.New("registry: provider name is required")
	}
	if def.Factory == nil {
		return errors.New("registry: provider factory is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providerItems[def.Name]; exists {
		return errors.New("registry: duplicate provider " + def.Name)
	}
	r.providerItems[def.Name] = def
	return nil
}

func (r *Registry) Get(name string) (provider.Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.providerItems[name]
	return def, ok
}

func (r *Registry) All() []provider.Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]provider.Definition, 0, len(r.providerItems))
	for _, item := range r.providerItems {
		out = append(out, item)
	}
	return out
}


