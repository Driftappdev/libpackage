package loader

import (
	"context"
	"errors"

	"github.com/driftappdev/libpackage/plugins/hooks"
	"github.com/driftappdev/libpackage/plugins/manifest"
)

type Loader interface {
	Load(m manifest.Manifest) error
}

// LifecyclePlugin is a runtime plugin contract for init/shutdown orchestration.
type LifecyclePlugin interface {
	Name() string
	Init(context.Context) error
	Close(context.Context) error
	Hooks() []hooks.Hook
}

// InitAll runs hook/plugin initialization in registration order.
func InitAll(ctx context.Context, plugins []LifecyclePlugin) error {
	for _, p := range plugins {
		for _, h := range p.Hooks() {
			if err := h.BeforeInit(ctx); err != nil {
				return err
			}
		}
		if err := p.Init(ctx); err != nil {
			return err
		}
		for _, h := range p.Hooks() {
			if err := h.AfterInit(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// CloseAll runs hook/plugin shutdown in reverse order.
func CloseAll(ctx context.Context, plugins []LifecyclePlugin) error {
	var hooksList []hooks.Hook
	for _, p := range plugins {
		hooksList = append(hooksList, p.Hooks()...)
	}

	for _, h := range hooksList {
		if err := h.BeforeShutdown(ctx); err != nil {
			return err
		}
	}

	for i := len(plugins) - 1; i >= 0; i-- {
		if err := plugins[i].Close(ctx); err != nil {
			return err
		}
	}

	for _, h := range hooksList {
		if err := h.AfterShutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Service orchestrates plugins using a registry function.
type Service struct {
	All func() []LifecyclePlugin
}

func (s Service) InitAll(ctx context.Context) error {
	if s.All == nil {
		return errors.New("loader: registry is nil")
	}
	return InitAll(ctx, s.All())
}

func (s Service) CloseAll(ctx context.Context) error {
	if s.All == nil {
		return errors.New("loader: registry is nil")
	}
	return CloseAll(ctx, s.All())
}
