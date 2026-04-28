package utils

// Must returns the value when err is nil, otherwise it panics.
func Must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// DefaultString returns fallback when value is empty.
func DefaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// MergeMaps shallow-merges maps from left to right.
func MergeMaps[K comparable, V any](items ...map[K]V) map[K]V {
	out := make(map[K]V)
	for _, m := range items {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
