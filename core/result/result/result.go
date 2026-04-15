package result

type Result[T any] struct {
	Value T
	Err   error
}

func Ok[T any](v T) Result[T] {
	return Result[T]{Value: v}
}

func Fail[T any](err error) Result[T] {
	return Result[T]{Err: err}
}

func From[T any](v T, err error) Result[T] {
	if err != nil {
		return Fail[T](err)
	}
	return Ok(v)
}

func (r Result[T]) IsOK() bool {
	return r.Err == nil
}

func (r Result[T]) IsOk() bool {
	return r.IsOK()
}

func (r Result[T]) IsErr() bool {
	return r.Err != nil
}

func (r Result[T]) Unwrap() (T, error) {
	return r.Value, r.Err
}

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

func (r Result[T]) ErrorOrNil() error {
	return r.Err
}

func (r Result[T]) OrElse(fn func(error) Result[T]) Result[T] {
	if r.Err == nil {
		return r
	}
	return fn(r.Err)
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

func Map[T any, U any](r Result[T], fn func(T) U) Result[U] {
	if r.Err != nil {
		return Fail[U](r.Err)
	}
	return Ok(fn(r.Value))
}

func FlatMap[T any, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if r.Err != nil {
		return Fail[U](r.Err)
	}
	return fn(r.Value)
}

func MapErr[T any](r Result[T], fn func(error) error) Result[T] {
	if r.Err == nil || fn == nil {
		return r
	}
	return Fail[T](fn(r.Err))
}
