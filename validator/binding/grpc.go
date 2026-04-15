package binding

import "context"

type GRPCValidator interface{ ValidateAll() error }

func ValidateGRPC(_ context.Context, in any) error {
	if v, ok := in.(GRPCValidator); ok {
		return v.ValidateAll()
	}
	return nil
}
