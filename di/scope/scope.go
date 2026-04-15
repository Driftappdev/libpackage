package scope

type Scope string

const (
	Singleton Scope = "singleton"
	Transient Scope = "transient"
	Request   Scope = "request"
)
