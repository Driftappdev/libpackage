package auth

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GRPCValidator func(ctx context.Context) error

func UnaryServerInterceptor(validate GRPCValidator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if validate != nil {
			if err := validate(ctx); err != nil {
				return nil, status.Error(codes.Unauthenticated, err.Error())
			}
		}
		return handler(ctx, req)
	}
}
