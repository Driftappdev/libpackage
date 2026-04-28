package response

type Envelope[T any] struct {
	Data    T         `json:"data"`
	Meta    any       `json:"meta,omitempty"`
	Error   *AppError `json:"error,omitempty"`
	TraceID string    `json:"trace_id,omitempty"`
}

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
