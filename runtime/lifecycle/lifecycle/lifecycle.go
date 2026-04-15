package lifecycle

import "context"

// Manager is a thin orchestration wrapper around App.
type Manager struct {
	app *App
}

func NewManager(components ...Component) *Manager {
	return &Manager{app: New(components...)}
}

func (m *Manager) Start(ctx context.Context) error {
	if m == nil || m.app == nil {
		return nil
	}
	return m.app.Start(ctx)
}

func (m *Manager) Stop(ctx context.Context) error {
	if m == nil || m.app == nil {
		return nil
	}
	return m.app.Stop(ctx)
}
