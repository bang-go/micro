package grpcx

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RecoveryHandlerContextFunc func(ctx context.Context, p any) error

func UnaryServerRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.UnaryServerInterceptor {
	if h == nil {
		h = defaultRecoveryHandler
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = h(ctx, r)
			}
		}()
		resp, err = handler(ctx, req)
		return
	}
}

func StreamServerRecoveryInterceptor(h RecoveryHandlerContextFunc) grpc.StreamServerInterceptor {
	if h == nil {
		h = defaultRecoveryHandler
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = h(stream.Context(), r)
			}
		}()
		err = handler(srv, stream)
		return err
	}
}

func defaultRecoveryHandler(context.Context, any) error {
	return status.Error(codes.Internal, "grpcx: recovered from panic")
}
