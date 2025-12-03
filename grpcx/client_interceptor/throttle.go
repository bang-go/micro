package client_interceptor

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ThrottleHandlerFunc func() bool

func UnaryClientThrottleInterceptor(h ThrottleHandlerFunc) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if !h() {
			return status.Errorf(codes.ResourceExhausted, "%s is rejected by grpc throttle control, please retry later", method)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func StreamClientThrottleInterceptor(h ThrottleHandlerFunc) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if !h() {
			return nil, status.Errorf(codes.ResourceExhausted, "%s is rejected by grpc throttle control, please retry later.", method)
		}
		return streamer(ctx, desc, cc, method, opts...)
	}
}
