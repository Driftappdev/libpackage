package recovery

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GRPC() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic recovered in %s: %v", info.FullMethod, r)
				err = status.Error(codes.Internal, fmt.Sprintf("panic recovered"))
			}
		}()
		return handler(ctx, req)
	}
}
