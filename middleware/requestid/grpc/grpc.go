package requestid

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func GRPC() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		_, _ = info, req
		md, _ := metadata.FromIncomingContext(ctx)
		id := ""
		if values := md.Get(Header); len(values) > 0 {
			id = values[0]
		}
		if id == "" {
			id = uuid.NewString()
		}
		ctx = metadata.AppendToOutgoingContext(ctx, Header, id)
		return handler(ctx, req)
	}
}
