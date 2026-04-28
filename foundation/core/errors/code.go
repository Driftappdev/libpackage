package errors

type Code string

const (
	CodeUnknown      Code = "UNKNOWN"
	CodeInvalid      Code = "INVALID"
	CodeNotFound     Code = "NOT_FOUND"
	CodeConflict     Code = "CONFLICT"
	CodeUnauthorized Code = "UNAUTHORIZED"
	CodeForbidden    Code = "FORBIDDEN"
	CodeTimeout      Code = "TIMEOUT"
	CodeUnavailable  Code = "UNAVAILABLE"
	CodeRateLimited  Code = "RATE_LIMITED"
	CodeInternal     Code = "INTERNAL"
)
