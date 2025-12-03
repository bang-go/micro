package client_interceptor

import (
	"context"

	"google.golang.org/grpc"
)

type RecoveryHandlerContextFunc func(ctx context.Context, p any)

func UnaryClientRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.UnaryClientInterceptor { //TODO:  参照grpc-middleware
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		defer func() {
			if r := recover(); r != nil {
				h(ctx, r)
			}
		}()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func StreamClientRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.StreamClientInterceptor { //TODO:  参照grpc-middleware
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		defer func() {
			if r := recover(); r != nil {
				h(ctx, r)
			}
		}()
		return streamer(ctx, desc, cc, method, opts...)
	}
}
