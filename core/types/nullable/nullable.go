package types

import "encoding/json"

type Nullable[T any] struct {
	Value T
	Valid bool
}

func Some[T any](v T) Nullable[T] { return Nullable[T]{Value: v, Valid: true} }
func None[T any]() Nullable[T]    { return Nullable[T]{} }

func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}
