package ratelimit

import (
	"context"
	"errors"

	core "github.com/driftappdev/libpackage/ratelimit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GRPCExtractor func(ctx context.Context, method string) string

func UnaryServerInterceptor(l *core.Limiter, extract GRPCExtractor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if l == nil {
			return handler(ctx, req)
		}
		subject := info.FullMethod
		if extract != nil {
			subject = extract(ctx, info.FullMethod)
		}
		_, err := l.Allow(ctx, core.Key{Namespace: "grpc", Identity: subject})
		if errors.Is(err, core.ErrLimited) {
			return nil, status.Error(codes.ResourceExhausted, "rate limited")
		}
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return handler(ctx, req)
	}
}
