package result

import (
	"errors"
	"fmt"
)

type Result[T any] struct {
	Value T
	Err   error
}

func Ok[T any](v T) Result[T]         { return Result[T]{Value: v} }
func Fail[T any](err error) Result[T] { return Result[T]{Err: err} }

func From[T any](v T, err error) Result[T] {
	if err != nil {
		return Fail[T](err)
	}
	return Ok(v)
}

func (r Result[T]) IsOK() bool         { return r.Err == nil }
func (r Result[T]) IsOk() bool         { return r.IsOK() }
func (r Result[T]) IsErr() bool        { return r.Err != nil }
func (r Result[T]) Unwrap() (T, error) { return r.Value, r.Err }

func (r Result[T]) Must() T {
	if r.Err != nil {
		panic(r.Err)
	}
	return r.Value
}

func (r Result[T]) ValueOr(fallback T) T {
	if r.Err != nil {
		return fallback
	}
	return r.Value
}

func (r Result[T]) ErrorOrNil() error { return r.Err }

func (r Result[T]) OrElse(fn func(error) Result[T]) Result[T] {
	if r.Err == nil || fn == nil {
		return r
	}
	return fn(r.Err)
}

func (r Result[T]) Recover(fn func(error) T) Result[T] {
	if r.Err == nil || fn == nil {
		return r
	}
	return Ok(fn(r.Err))
}

func (r Result[T]) Tap(fn func(T)) Result[T] {
	if r.Err == nil && fn != nil {
		fn(r.Value)
	}
	return r
}

func (r Result[T]) TapErr(fn func(error)) Result[T] {
	if r.Err != nil && fn != nil {
		fn(r.Err)
	}
	return r
}

func (r Result[T]) WrapErr(msg string) Result[T] {
	if r.Err == nil || msg == "" {
		return r
	}
	return Fail[T](fmt.Errorf("%s: %w", msg, r.Err))
}

func (r Result[T]) OnErr(target error, fallback T) Result[T] {
	if r.Err == nil {
		return r
	}
	if errors.Is(r.Err, target) {
		return Ok(fallback)
	}
	return r
}

func Map[T any, U any](r Result[T], fn func(T) U) Result[U] {
	if r.Err != nil {
		return Fail[U](r.Err)
	}
	if fn == nil {
		return Fail[U](errors.New("map: fn is nil"))
	}
	return Ok(fn(r.Value))
}

func FlatMap[T any, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if r.Err != nil {
		return Fail[U](r.Err)
	}
	if fn == nil {
		return Fail[U](errors.New("flatmap: fn is nil"))
	}
	return fn(r.Value)
}

func MapErr[T any](r Result[T], fn func(error) error) Result[T] {
	if r.Err == nil || fn == nil {
		return r
	}
	return Fail[T](fn(r.Err))
}

func Combine2[A any, B any](a Result[A], b Result[B]) Result[struct {
	A A
	B B
}] {
	if a.Err != nil {
		return Fail[struct {
			A A
			B B
		}](a.Err)
	}
	if b.Err != nil {
		return Fail[struct {
			A A
			B B
		}](b.Err)
	}
	return Ok(struct {
		A A
		B B
	}{A: a.Value, B: b.Value})
}

func All[T any](results ...Result[T]) Result[[]T] {
	out := make([]T, 0, len(results))
	for _, r := range results {
		if r.Err != nil {
			return Fail[[]T](r.Err)
		}
		out = append(out, r.Value)
	}
	return Ok(out)
}
