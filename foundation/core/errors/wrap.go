package errors

func Wrap(err error, code Code, message string) *Error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Message: message, Cause: err}
}

func WithMeta(err *Error, key string, value any) *Error {
	if err == nil {
		return nil
	}
	if err.Meta == nil {
		err.Meta = map[string]any{}
	}
	err.Meta[key] = value
	return err
}
