package provider

import "context"

type Memory struct{ values map[string]bool }

func NewMemory(values map[string]bool) *Memory { return &Memory{values: values} }

func (m *Memory) Bool(_ context.Context, key string, fallback bool) bool {
	v, ok := m.values[key]
	if !ok {
		return fallback
	}
	return v
}
