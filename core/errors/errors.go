package errors

import stderrors "errors"

// Is reports whether err is a library Error with the given code.
func Is(err error, code Code) bool {
	e, ok := As(err)
	return ok && e.Code == code
}

// As extracts *Error from an arbitrary error chain.
func As(err error) (*Error, bool) {
	var target *Error
	if stderrors.As(err, &target) {
		return target, true
	}
	return nil, false
}
