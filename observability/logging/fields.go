package logging

type Field struct {
	Key   string
	Value any
}

func Map(fields ...Field) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		out[f.Key] = f.Value
	}
	return out
}
