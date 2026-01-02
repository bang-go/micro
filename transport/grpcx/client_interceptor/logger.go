package grpcx

import (
	"context"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryClientLoggerInterceptor(l *logger.Logger) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		duration := time.Since(start)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		l.Info(ctx, "grpc_access_log",
			"kind", "client",
			"method", method,
			"code", code.String(),
			"cost", duration.Seconds(),
			"error", err,
		)

		return err
	}
}

func StreamClientLoggerInterceptor(l *logger.Logger) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		start := time.Now()
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		duration := time.Since(start)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		l.Info(ctx, "grpc_access_log",
			"kind", "client",
			"method", method,
			"code", code.String(),
			"duration", duration.String(),
			"error", err,
		)

		return clientStream, err
	}
}
