package baggage

import "context"

type contextKey string

const baggageKey contextKey = "midul.baggage"

func With(ctx context.Context, key, value string) context.Context {
	current := FromContext(ctx)
	next := map[string]string{}
	for k, v := range current {
		next[k] = v
	}
	next[key] = value
	return context.WithValue(ctx, baggageKey, next)
}

func FromContext(ctx context.Context) map[string]string {
	v, _ := ctx.Value(baggageKey).(map[string]string)
	if v == nil {
		return map[string]string{}
	}
	return v
}
