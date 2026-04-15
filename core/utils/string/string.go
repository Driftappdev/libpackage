package utils

import "strings"

func Trimmed(v string) string { return strings.TrimSpace(v) }
func IsBlank(v string) bool   { return strings.TrimSpace(v) == "" }
func Coalesce(values ...string) string {
	for _, v := range values {
		if !IsBlank(v) {
			return v
		}
	}
	return ""
}
