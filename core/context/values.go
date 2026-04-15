package context

import "context"

func With(ctx context.Context, key Key, value string) context.Context {
	return context.WithValue(ctx, key, value)
}

func Get(ctx context.Context, key Key) string {
	v, _ := ctx.Value(key).(string)
	return v
}
