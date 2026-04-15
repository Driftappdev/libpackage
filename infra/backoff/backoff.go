package backoff

import "time"

func Exponential(base, max time.Duration, multiplier float64, attempt int) time.Duration {
	if attempt <= 1 {
		return base
	}
	delay := float64(base)
	for i := 1; i < attempt; i++ {
		delay *= multiplier
	}
	out := time.Duration(delay)
	if max > 0 && out > max {
		return max
	}
	return out
}
