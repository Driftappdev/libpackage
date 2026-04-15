package grpc

import (
	"context"

	ggrpc "google.golang.org/grpc"
)

func UnaryClientInterceptor(extraMD map[string]string) ggrpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *ggrpc.ClientConn, invoker ggrpc.UnaryInvoker, opts ...ggrpc.CallOption) error {
		_, _ = ctx, extraMD
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
