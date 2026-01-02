package grpcx_test

import (
	"context"
	"testing"

	serverinterceptor "github.com/bang-go/micro/transport/grpcx/server_interceptor"
	"google.golang.org/grpc"
)

func TestRecovery(t *testing.T) {
	custom := func(ctx context.Context, p any) {}
	grpc.NewServer(grpc.ChainUnaryInterceptor(serverinterceptor.UnaryServerRecoveryInterceptor(custom)))
}
