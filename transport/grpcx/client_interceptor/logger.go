package grpcx

import (
	"context"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"google.golang.org/grpc"
)

func UnaryClientLoggerInterceptor(l *logger.Logger) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		duration := time.Since(start)

		l.Info(ctx, "grpc_access_log",
			"kind", "client",
			"method", method,
			"code", streamStatusCode(err).String(),
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
		if err != nil {
			logClientStream(l, ctx, method, time.Since(start), err)
			return nil, err
		}

		return newObservedClientStream(clientStream, func(streamErr error) {
			logClientStream(l, ctx, method, time.Since(start), streamErr)
		}), nil
	}
}

func logClientStream(l *logger.Logger, ctx context.Context, method string, duration time.Duration, err error) {
	l.Info(ctx, "grpc_access_log",
		"kind", "client",
		"method", method,
		"code", streamStatusCode(err).String(),
		"cost", duration.Seconds(),
		"error", err,
	)
}
