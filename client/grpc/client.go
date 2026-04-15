package grpc

import (
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Dial(address string, interceptors ...ggrpc.UnaryClientInterceptor) (*ggrpc.ClientConn, error) {
	opts := []ggrpc.DialOption{ggrpc.WithTransportCredentials(insecure.NewCredentials()), ggrpc.WithChainUnaryInterceptor(interceptors...)}
	return ggrpc.NewClient(address, opts...)
}
