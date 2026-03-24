package grpcx_test

import (
	"context"
	"errors"
	"testing"

	clientinterceptor "github.com/bang-go/micro/transport/grpcx/client_interceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnaryClientRecoveryInterceptor(t *testing.T) {
	var recovered bool

	interceptor := clientinterceptor.UnaryClientRecoveryInterceptor(func(ctx context.Context, p any) error {
		recovered = true
		return status.Error(codes.Internal, "client panic recovered")
	})

	err := interceptor(context.Background(), "/svc/method", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		panic("boom")
	})

	if !recovered {
		t.Fatal("expected recovery handler to run")
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("status.Code(err) = %v, want %v", status.Code(err), codes.Internal)
	}
}

func TestUnaryClientRecoveryInterceptorNilHandler(t *testing.T) {
	interceptor := clientinterceptor.UnaryClientRecoveryInterceptor(nil)

	err := interceptor(context.Background(), "/svc/method", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		panic("boom")
	})

	if status.Code(err) != codes.Internal {
		t.Fatalf("status.Code(err) = %v, want %v", status.Code(err), codes.Internal)
	}
}

func TestUnaryClientRecoveryInterceptorPassthrough(t *testing.T) {
	want := errors.New("call failed")
	interceptor := clientinterceptor.UnaryClientRecoveryInterceptor(nil)

	err := interceptor(context.Background(), "/svc/method", nil, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		return want
	})

	if !errors.Is(err, want) {
		t.Fatalf("expected original error, got %v", err)
	}
}
