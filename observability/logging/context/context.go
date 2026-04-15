package logging

import "context"

type contextKey string

const fieldsKey contextKey = "midul.log.fields"

func WithField(ctx context.Context, key string, value any) context.Context {
	current := FieldsFromContext(ctx)
	next := map[string]any{}
	for k, v := range current {
		next[k] = v
	}
	next[key] = value
	return context.WithValue(ctx, fieldsKey, next)
}

func FieldsFromContext(ctx context.Context) map[string]any {
	v, _ := ctx.Value(fieldsKey).(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}
