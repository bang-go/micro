package grpcx

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RecoveryHandlerContextFunc func(ctx context.Context, p any) error

func UnaryClientRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.UnaryClientInterceptor {
	if h == nil {
		h = defaultRecoveryHandler
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = h(ctx, r)
			}
		}()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func StreamClientRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.StreamClientInterceptor {
	if h == nil {
		h = defaultRecoveryHandler
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		var (
			clientStream grpc.ClientStream
			err          error
		)
		defer func() {
			if r := recover(); r != nil {
				err = h(ctx, r)
			}
		}()
		clientStream, err = streamer(ctx, desc, cc, method, opts...)
		return clientStream, err
	}
}

func defaultRecoveryHandler(context.Context, any) error {
	return status.Error(codes.Internal, "grpcx: recovered from panic")
}
