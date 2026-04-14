package logging

import (
	"context"
	"time"

	corelog "github.com/driftappdev/libpackage/core/logger"
	"google.golang.org/grpc"
)

func UnaryServerInterceptor(log corelog.Logger) grpc.UnaryServerInterceptor {
	if log == nil {
		log = corelog.New(corelog.Options{})
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		start := time.Now()
		resp, err = handler(ctx, req)
		fields := []corelog.Field{
			corelog.String("method", info.FullMethod),
			corelog.Int64("duration_ms", time.Since(start).Milliseconds()),
		}
		if err != nil {
			fields = append(fields, corelog.Error(err))
			log.ErrorContext(ctx, "grpc_request", fields...)
			return resp, err
		}
		log.InfoContext(ctx, "grpc_request", fields...)
		return resp, nil
	}
}
