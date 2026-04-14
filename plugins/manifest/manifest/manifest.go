package manifest

type Manifest struct {
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Entrypoint  string            `json:"entrypoint" yaml:"entrypoint"`
	Metadata    map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}
