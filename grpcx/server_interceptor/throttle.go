package server_interceptor

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ThrottleHandlerFunc func() bool

func UnaryServerThrottleInterceptor(h ThrottleHandlerFunc) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		if !h() {
			return nil, status.Errorf(codes.ResourceExhausted, "%s is rejected by grpc throttle control, please retry later", info.FullMethod)
		}
		return handler(ctx, req)
	}
}

func StreamServerThrottleInterceptor(h ThrottleHandlerFunc) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !h() {
			return status.Errorf(codes.ResourceExhausted, "%s is rejected by grpc throttle control, please retry later.", info.FullMethod)
		}
		return handler(srv, stream)
	}
}
