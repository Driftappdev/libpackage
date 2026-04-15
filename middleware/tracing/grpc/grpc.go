package tracing

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if len(md.Get("traceparent")) == 0 {
			md = md.Copy()
			md.Set("traceparent", info.FullMethod)
			ctx = metadata.NewIncomingContext(ctx, md)
		}
		return handler(ctx, req)
	}
}
