package utils

func Ptr[T any](v T) *T { return &v }
func Deref[T any](v *T, fallback T) T {
	if v == nil {
		return fallback
	}
	return *v
}
