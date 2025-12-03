package server_interceptor

import (
	"context"

	"google.golang.org/grpc"
)

type RecoveryHandlerContextFunc func(ctx context.Context, p any)

func UnaryServerRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.UnaryServerInterceptor { //TODO:  参照grpc-middleware
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				h(ctx, r)
			}
		}()
		resp, err = handler(ctx, req)
		return
	}
}

func StreamServerRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.StreamServerInterceptor { //TODO:  参照grpc-middleware
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		var err error
		defer func() {
			if r := recover(); r != nil {
				h(stream.Context(), r)
			}
		}()
		err = handler(srv, stream)
		return err
	}
}
