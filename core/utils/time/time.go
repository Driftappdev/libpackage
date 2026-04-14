package utils

import "time"

func MustRFC3339(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		panic(err)
	}
	return t
}

func TruncateToSecond(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
