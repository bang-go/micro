package grpcx

import (
	"context"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryServerLoggerInterceptor(l *logger.Logger, skipMethods ...string) grpc.UnaryServerInterceptor {
	skip := make(map[string]struct{})
	for _, m := range skipMethods {
		skip[m] = struct{}{}
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Skip logging for specified methods
		if _, ok := skip[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		l.Info(ctx, "grpc_access_log",
			"kind", "server",
			"method", info.FullMethod,
			"code", code.String(),
			"cost", duration.Seconds(),
			"error", err,
		)

		return resp, err
	}
}

func StreamServerLoggerInterceptor(l *logger.Logger, skipMethods ...string) grpc.StreamServerInterceptor {
	skip := make(map[string]struct{})
	for _, m := range skipMethods {
		skip[m] = struct{}{}
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Skip logging for specified methods
		if _, ok := skip[info.FullMethod]; ok {
			return handler(srv, stream)
		}

		start := time.Now()
		err := handler(srv, stream)
		duration := time.Since(start)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		l.Info(context.Background(), "grpc_access_log",
			"kind", "server",
			"method", info.FullMethod,
			"code", code.String(),
			"cost", duration.Seconds(),
			"error", err,
		)

		return err
	}
}
