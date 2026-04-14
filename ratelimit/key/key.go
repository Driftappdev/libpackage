package ratelimit

type Key struct {
	Namespace string
	Identity  string
}

func (k Key) String() string {
	if k.Namespace == "" {
		return k.Identity
	}
	return k.Namespace + ":" + k.Identity
}
