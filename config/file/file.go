package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

func LoadYAML[T any](path string, target *T) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(raw, target)
}
