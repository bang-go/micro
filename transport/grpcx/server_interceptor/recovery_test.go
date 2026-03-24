package grpcx_test

import (
	"context"
	"errors"
	"testing"

	serverinterceptor "github.com/bang-go/micro/transport/grpcx/server_interceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnaryServerRecoveryInterceptor(t *testing.T) {
	var recovered bool

	interceptor := serverinterceptor.UnaryServerRecoveryInterceptor(func(ctx context.Context, p any) error {
		recovered = true
		return status.Error(codes.Internal, "server panic recovered")
	})

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/method"}, func(context.Context, any) (any, error) {
		panic("boom")
	})

	if !recovered {
		t.Fatal("expected recovery handler to run")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("status.Code(err) = %v, want %v", status.Code(err), codes.Internal)
	}
}

func TestUnaryServerRecoveryInterceptorNilHandler(t *testing.T) {
	interceptor := serverinterceptor.UnaryServerRecoveryInterceptor(nil)

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/method"}, func(context.Context, any) (any, error) {
		panic("boom")
	})

	if status.Code(err) != codes.Internal {
		t.Fatalf("status.Code(err) = %v, want %v", status.Code(err), codes.Internal)
	}
}

func TestUnaryServerRecoveryInterceptorPassthrough(t *testing.T) {
	want := errors.New("handler failed")
	interceptor := serverinterceptor.UnaryServerRecoveryInterceptor(nil)

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/method"}, func(context.Context, any) (any, error) {
		return nil, want
	})

	if !errors.Is(err, want) {
		t.Fatalf("expected original error, got %v", err)
	}
}
