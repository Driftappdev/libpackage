package context

type Key string

const (
	RequestIDKey     Key = "request_id"
	CorrelationIDKey Key = "correlation_id"
	TenantIDKey      Key = "tenant_id"
	PrincipalIDKey   Key = "principal_id"
)
