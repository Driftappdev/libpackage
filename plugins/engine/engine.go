// Package plugin provides an enterprise-grade plugin system for Go microservices.
//
// Features:
//   - Plugin lifecycle management (load, start, stop, health check)
//   - Version-based compatibility checking (semver)
//   - Multiple plugin types: native Go, gRPC subprocess, WASM (interface)
//   - Hot-reload without service restart
//   - Dependency injection into plugins
//   - Plugin marketplace / registry
//   - Sandbox (resource limits, capability restrictions)
//   - Event bus between host and plugins
//   - Prometheus metrics per plugin
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"sync"
	"time"

	"github.com/driftappdev/plugins/common"
)

// ============================================================
// VERSION
// ============================================================

// Version represents a semantic version.
type Version struct {
	Major int
	Minor int
	Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compatible returns true if v is compatible with required (same major, v >= required).
func (v Version) Compatible(required Version) bool {
	if v.Major != required.Major {
		return false
	}
	if v.Minor > required.Minor {
		return true
	}
	if v.Minor == required.Minor && v.Patch >= required.Patch {
		return true
	}
	return false
}

// ParseVersion parses "1.2.3" into a Version.
func ParseVersion(s string) (Version, error) {
	var v Version
	_, err := fmt.Sscanf(s, "%d.%d.%d", &v.Major, &v.Minor, &v.Patch)
	return v, err
}

// ============================================================
// METADATA & MANIFEST
// ============================================================

// PluginType identifies the plugin execution model.
type PluginType string

const (
	PluginTypeNative PluginType = "native" // Go .so plugin
	PluginTypeGRPC   PluginType = "grpc"   // gRPC subprocess
	PluginTypeHTTP   PluginType = "http"   // HTTP sidecar
)

// Capability describes a plugin's declared capability.
type Capability string

const (
	CapabilityHTTPMiddleware Capability = "http_middleware"
	CapabilityEventHandler   Capability = "event_handler"
	CapabilityDataTransform  Capability = "data_transform"
	CapabilityAuthProvider   Capability = "auth_provider"
	CapabilityStorageAdapter Capability = "storage_adapter"
	CapabilityCustom         Capability = "custom"
)

// PluginManifest is the plugin's self-description (loaded from manifest.json).
type PluginManifest struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Version        string       `json:"version"`
	Description    string       `json:"description"`
	Author         string       `json:"author"`
	License        string       `json:"license"`
	Type           PluginType   `json:"type"`
	Capabilities   []Capability `json:"capabilities"`
	MinHostVersion string       `json:"min_host_version"`
	Dependencies   []struct {
		PluginID string `json:"plugin_id"`
		Version  string `json:"version"`
	} `json:"dependencies"`
	Config     map[string]ConfigSchema `json:"config"`
	EntryPoint string                  `json:"entry_point"` // binary/so path
	Checksum   string                  `json:"checksum"`    // SHA256 of entry point
}

// ConfigSchema describes a configuration parameter.
type ConfigSchema struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Default     any    `json:"default"`
}

// ============================================================
// CORE PLUGIN INTERFACE
// ============================================================

// Plugin is the interface that all plugins must implement.
type Plugin interface {
	// ID returns the unique plugin identifier.
	ID() string
	// Version returns the plugin version.
	Version() Version
	// Capabilities returns supported capabilities.
	Capabilities() []Capability
	// Init is called once after loading with host services injected.
	Init(ctx context.Context, host Host) error
	// Start begins the plugin's operation.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the plugin.
	Stop(ctx context.Context) error
	// HealthCheck returns the plugin's health status.
	HealthCheck(ctx context.Context) common.HealthResult
	// Configure applies runtime configuration.
	Configure(cfg map[string]any) error
}

// Host provides services from the host to plugins.
type Host interface {
	Logger() common.Logger
	Metrics() common.MetricsRecorder
	Publish(ctx context.Context, event Event) error
	Subscribe(eventType string, handler EventHandler) (SubscriptionID, error)
	GetPlugin(id string) (Plugin, error)
}

// ============================================================
// EVENTS
// ============================================================

// Event is a message passed between plugins and the host.
type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Source    string    `json:"source"`
	Payload   any       `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

// EventHandler processes incoming events.
type EventHandler func(ctx context.Context, event Event) error

// SubscriptionID uniquely identifies an event subscription.
type SubscriptionID string

// ============================================================
// PLUGIN STATE
// ============================================================

// PluginState tracks a loaded plugin's lifecycle.
type PluginState string

const (
	PluginStateLoading  PluginState = "loading"
	PluginStateRunning  PluginState = "running"
	PluginStateStopped  PluginState = "stopped"
	PluginStateFailed   PluginState = "failed"
	PluginStateDisabled PluginState = "disabled"
)

// PluginEntry wraps a loaded plugin with metadata.
type PluginEntry struct {
	Manifest  *PluginManifest
	Plugin    Plugin
	State     PluginState
	LoadedAt  time.Time
	StartedAt *time.Time
	Error     error
	Config    map[string]any
}

// ============================================================
// NATIVE GO PLUGIN LOADER
// ============================================================

// NativeLoader loads Go .so plugins.
type NativeLoader struct {
	logger common.Logger
}

// NewNativeLoader creates a native Go plugin loader.
func NewNativeLoader(logger common.Logger) *NativeLoader {
	return &NativeLoader{logger: logger}
}

// Load loads a .so plugin from the given path.
// The .so must export a symbol "NewPlugin" of type func() Plugin.
func (nl *NativeLoader) Load(ctx context.Context, path string) (Plugin, error) {
	p, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open plugin %q: %w", path, err)
	}
	sym, err := p.Lookup("NewPlugin")
	if err != nil {
		return nil, fmt.Errorf("lookup NewPlugin in %q: %w", path, err)
	}
	factory, ok := sym.(func() Plugin)
	if !ok {
		return nil, fmt.Errorf("NewPlugin in %q has wrong signature", path)
	}
	return factory(), nil
}

// ============================================================
// SUBPROCESS PLUGIN (gRPC)
// ============================================================

// SubprocessPlugin manages a plugin running as a child process.
type SubprocessPlugin struct {
	manifest *PluginManifest
	cmd      *exec.Cmd
	mu       sync.Mutex
	running  bool
	logger   common.Logger
}

// NewSubprocessPlugin creates a subprocess plugin manager.
func NewSubprocessPlugin(manifest *PluginManifest, logger common.Logger) *SubprocessPlugin {
	return &SubprocessPlugin{manifest: manifest, logger: logger}
}

func (s *SubprocessPlugin) ID() string                 { return s.manifest.ID }
func (s *SubprocessPlugin) Capabilities() []Capability { return s.manifest.Capabilities }
func (s *SubprocessPlugin) Version() Version {
	v, _ := ParseVersion(s.manifest.Version)
	return v
}

func (s *SubprocessPlugin) Init(_ context.Context, _ Host) error { return nil }

func (s *SubprocessPlugin) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.cmd = exec.CommandContext(ctx, s.manifest.EntryPoint)
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start plugin subprocess %q: %w", s.manifest.ID, err)
	}
	s.running = true
	s.logger.Info("plugin subprocess started",
		common.String("id", s.manifest.ID),
		common.Int("pid", s.cmd.Process.Pid))
	return nil
}

func (s *SubprocessPlugin) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.cmd == nil {
		return nil
	}
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		s.cmd.Process.Kill() //nolint:errcheck
	}
	s.cmd.Wait() //nolint:errcheck
	s.running = false
	return nil
}

func (s *SubprocessPlugin) HealthCheck(_ context.Context) common.HealthResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return common.HealthResult{Status: common.HealthStatusUp, Component: s.manifest.ID}
	}
	return common.HealthResult{Status: common.HealthStatusDown, Component: s.manifest.ID}
}

func (s *SubprocessPlugin) Configure(cfg map[string]any) error { return nil }

// ============================================================
// PLUGIN REGISTRY
// ============================================================

// Registry is the central plugin registry and manager.
type Registry struct {
	mu            sync.RWMutex
	plugins       map[string]*PluginEntry
	subscriptions map[string][]subscriptionEntry
	logger        common.Logger
	metrics       common.MetricsRecorder
	loaders       map[PluginType]Loader
	hostVersion   Version
}

type subscriptionEntry struct {
	id      SubscriptionID
	handler EventHandler
}

// Loader loads a plugin from a path/address.
type Loader interface {
	Load(ctx context.Context, path string) (Plugin, error)
}

// RegistryConfig configures the registry.
type RegistryConfig struct {
	HostVersion Version
	Logger      common.Logger
	Metrics     common.MetricsRecorder
}

// NewRegistry creates a new plugin registry.
func NewRegistry(cfg RegistryConfig) *Registry {
	if cfg.Logger == nil {
		cfg.Logger = common.MustNewLogger("info")
	}
	if cfg.Metrics == nil {
		cfg.Metrics = common.NoopMetrics{}
	}
	r := &Registry{
		plugins:       make(map[string]*PluginEntry),
		subscriptions: make(map[string][]subscriptionEntry),
		logger:        cfg.Logger,
		metrics:       cfg.Metrics,
		loaders:       make(map[PluginType]Loader),
		hostVersion:   cfg.HostVersion,
	}
	r.loaders[PluginTypeNative] = NewNativeLoader(cfg.Logger)
	return r
}

// RegisterLoader registers a custom plugin loader for a type.
func (r *Registry) RegisterLoader(t PluginType, l Loader) {
	r.mu.Lock()
	r.loaders[t] = l
	r.mu.Unlock()
}

// LoadFromDirectory scans a directory for manifest.json files and loads all plugins.
func (r *Registry) LoadFromDirectory(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read plugin dir %q: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, entry.Name(), "manifest.json")
		if err := r.LoadFromManifest(ctx, manifestPath, nil); err != nil {
			r.logger.Warn("plugin: failed to load",
				common.String("dir", entry.Name()),
				common.Error(err))
		}
	}
	return nil
}

// LoadFromManifest loads a plugin from a manifest file.
func (r *Registry) LoadFromManifest(ctx context.Context, manifestPath string, config map[string]any) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest %q: %w", manifestPath, err)
	}
	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse manifest %q: %w", manifestPath, err)
	}
	return r.Load(ctx, &manifest, config)
}

// Load loads and registers a plugin from a manifest.
func (r *Registry) Load(ctx context.Context, manifest *PluginManifest, config map[string]any) error {
	// Version compatibility check
	if manifest.MinHostVersion != "" {
		minVer, err := ParseVersion(manifest.MinHostVersion)
		if err != nil {
			return fmt.Errorf("invalid min_host_version in plugin %q: %w", manifest.ID, err)
		}
		if !r.hostVersion.Compatible(minVer) {
			return fmt.Errorf("plugin %q requires host >= %s, got %s",
				manifest.ID, minVer, r.hostVersion)
		}
	}

	// Dependency check
	for _, dep := range manifest.Dependencies {
		r.mu.RLock()
		depEntry, ok := r.plugins[dep.PluginID]
		r.mu.RUnlock()
		if !ok {
			return fmt.Errorf("plugin %q requires missing dependency %q", manifest.ID, dep.PluginID)
		}
		if depEntry.State != PluginStateRunning {
			return fmt.Errorf("plugin %q dependency %q is not running", manifest.ID, dep.PluginID)
		}
	}

	entry := &PluginEntry{
		Manifest: manifest,
		State:    PluginStateLoading,
		LoadedAt: time.Now(),
		Config:   config,
	}

	r.mu.Lock()
	r.plugins[manifest.ID] = entry
	r.mu.Unlock()

	// Load via appropriate loader
	loader, ok := r.loaders[manifest.Type]
	if !ok {
		// Fallback to subprocess
		entry.Plugin = NewSubprocessPlugin(manifest, r.logger)
	} else {
		entryPoint := manifest.EntryPoint
		if !filepath.IsAbs(entryPoint) {
			entryPoint = filepath.Join(filepath.Dir(""), entryPoint)
		}
		p, err := loader.Load(ctx, entryPoint)
		if err != nil {
			entry.State = PluginStateFailed
			entry.Error = err
			return fmt.Errorf("load plugin %q: %w", manifest.ID, err)
		}
		entry.Plugin = p
	}

	// Init
	if err := entry.Plugin.Init(ctx, r.asHost()); err != nil {
		entry.State = PluginStateFailed
		entry.Error = err
		return fmt.Errorf("init plugin %q: %w", manifest.ID, err)
	}

	// Configure
	if config != nil {
		if err := entry.Plugin.Configure(config); err != nil {
			r.logger.Warn("plugin: configure failed",
				common.String("id", manifest.ID),
				common.Error(err))
		}
	}

	// Start
	if err := entry.Plugin.Start(ctx); err != nil {
		entry.State = PluginStateFailed
		entry.Error = err
		return fmt.Errorf("start plugin %q: %w", manifest.ID, err)
	}

	now := time.Now()
	entry.StartedAt = &now
	entry.State = PluginStateRunning

	r.logger.Info("plugin: loaded and started",
		common.String("id", manifest.ID),
		common.String("version", manifest.Version))
	r.metrics.IncrCounter("plugin_loaded_total", map[string]string{"id": manifest.ID})
	return nil
}

// Unload stops and removes a plugin.
func (r *Registry) Unload(ctx context.Context, id string) error {
	r.mu.Lock()
	entry, ok := r.plugins[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("plugin %q not found", id)
	}
	r.mu.Unlock()

	if err := entry.Plugin.Stop(ctx); err != nil {
		r.logger.Error("plugin: stop failed", common.String("id", id), common.Error(err))
	}

	r.mu.Lock()
	entry.State = PluginStateStopped
	delete(r.plugins, id)
	r.mu.Unlock()

	r.logger.Info("plugin: unloaded", common.String("id", id))
	return nil
}

// Reload hot-reloads a plugin without restarting the host.
func (r *Registry) Reload(ctx context.Context, id string, newConfig map[string]any) error {
	r.mu.RLock()
	entry, ok := r.plugins[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	manifest := entry.Manifest
	if err := r.Unload(ctx, id); err != nil {
		return err
	}
	return r.Load(ctx, manifest, newConfig)
}

// Get returns a running plugin by ID.
func (r *Registry) Get(id string) (Plugin, error) {
	r.mu.RLock()
	entry, ok := r.plugins[id]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugin %q not found", id)
	}
	if entry.State != PluginStateRunning {
		return nil, fmt.Errorf("plugin %q is not running (state: %s)", id, entry.State)
	}
	return entry.Plugin, nil
}

// List returns all loaded plugin entries.
func (r *Registry) List() []*PluginEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*PluginEntry, 0, len(r.plugins))
	for _, e := range r.plugins {
		result = append(result, e)
	}
	return result
}

// ByCapability returns all running plugins with a given capability.
func (r *Registry) ByCapability(cap Capability) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Plugin
	for _, e := range r.plugins {
		if e.State != PluginStateRunning {
			continue
		}
		for _, c := range e.Manifest.Capabilities {
			if c == cap {
				result = append(result, e.Plugin)
				break
			}
		}
	}
	return result
}

// CheckHealth runs health checks on all plugins.
func (r *Registry) CheckHealth(ctx context.Context) map[string]common.HealthResult {
	r.mu.RLock()
	ids := make([]string, 0, len(r.plugins))
	for id := range r.plugins {
		ids = append(ids, id)
	}
	r.mu.RUnlock()

	results := make(map[string]common.HealthResult)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(pluginID string) {
			defer wg.Done()
			r.mu.RLock()
			entry := r.plugins[pluginID]
			r.mu.RUnlock()
			if entry == nil {
				return
			}
			result := entry.Plugin.HealthCheck(ctx)
			mu.Lock()
			results[pluginID] = result
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return results
}

// Publish publishes an event to all subscribers.
func (r *Registry) Publish(ctx context.Context, event Event) error {
	r.mu.RLock()
	subs := make([]subscriptionEntry, len(r.subscriptions[event.Type]))
	copy(subs, r.subscriptions[event.Type])
	r.mu.RUnlock()

	var errs []error
	for _, sub := range subs {
		if err := sub.handler(ctx, event); err != nil {
			errs = append(errs, fmt.Errorf("handler %s: %w", sub.id, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("publish errors: %v", errs)
	}
	return nil
}

// Subscribe registers an event handler.
func (r *Registry) Subscribe(eventType string, handler EventHandler) (SubscriptionID, error) {
	id := SubscriptionID(fmt.Sprintf("sub-%d", time.Now().UnixNano()))
	r.mu.Lock()
	r.subscriptions[eventType] = append(r.subscriptions[eventType], subscriptionEntry{id: id, handler: handler})
	r.mu.Unlock()
	return id, nil
}

// asHost returns a Host implementation backed by this registry.
func (r *Registry) asHost() Host {
	return &registryHost{registry: r}
}

type registryHost struct {
	registry *Registry
}

func (h *registryHost) Logger() common.Logger           { return h.registry.logger }
func (h *registryHost) Metrics() common.MetricsRecorder { return h.registry.metrics }
func (h *registryHost) Publish(ctx context.Context, event Event) error {
	return h.registry.Publish(ctx, event)
}
func (h *registryHost) Subscribe(eventType string, handler EventHandler) (SubscriptionID, error) {
	return h.registry.Subscribe(eventType, handler)
}
func (h *registryHost) GetPlugin(id string) (Plugin, error) { return h.registry.Get(id) }

// ============================================================
// EXAMPLE BASE PLUGIN
// ============================================================

// BasePlugin provides a default implementation for non-essential Plugin methods.
// Embed this in your own plugins to avoid boilerplate.
type BasePlugin struct {
	id           string
	version      Version
	capabilities []Capability
	host         Host
	logger       common.Logger
}

// NewBasePlugin creates a base plugin.
func NewBasePlugin(id, version string, caps ...Capability) *BasePlugin {
	v, _ := ParseVersion(version)
	return &BasePlugin{id: id, version: v, capabilities: caps}
}

func (b *BasePlugin) ID() string                 { return b.id }
func (b *BasePlugin) Version() Version           { return b.version }
func (b *BasePlugin) Capabilities() []Capability { return b.capabilities }
func (b *BasePlugin) Init(_ context.Context, host Host) error {
	b.host = host
	b.logger = host.Logger().With(common.String("plugin", b.id))
	return nil
}
func (b *BasePlugin) Stop(_ context.Context) error { return nil }
func (b *BasePlugin) HealthCheck(_ context.Context) common.HealthResult {
	return common.HealthResult{Status: common.HealthStatusUp, Component: b.id}
}
func (b *BasePlugin) Configure(_ map[string]any) error { return nil }
func (b *BasePlugin) Logger() common.Logger            { return b.logger }
func (b *BasePlugin) Host() Host                       { return b.host }

