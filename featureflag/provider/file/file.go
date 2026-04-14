package provider

import (
	"context"
	"encoding/json"
	"os"
)

type File struct{ flags map[string]bool }

func NewFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]bool{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return &File{flags: values}, nil
}

func (f *File) Bool(_ context.Context, key string, fallback bool) bool {
	v, ok := f.flags[key]
	if !ok {
		return fallback
	}
	return v
}
