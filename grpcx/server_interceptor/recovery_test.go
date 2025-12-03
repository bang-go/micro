package server_interceptor_test

import (
	"context"
	"testing"

	"github.com/bang-go/micro/grpcx/server_interceptor"
	"google.golang.org/grpc"
)

func TestRecovery(t *testing.T) {
	custom := func(ctx context.Context, p any) {}
	grpc.NewServer(grpc.ChainUnaryInterceptor(server_interceptor.UnaryServerRecoveryInterceptor(custom)))
}
