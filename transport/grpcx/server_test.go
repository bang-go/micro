package grpcx_test

import (
	"log"
	"testing"

	"github.com/bang-go/micro/transport/grpcx"
	"google.golang.org/grpc"
)

func TestServerStart(t *testing.T) {
	server := grpcx.NewServer(&grpcx.ServerConfig{Addr: "127.0.0.1:8081"})
	err := server.Start(func(server *grpc.Server) {})
	if err != nil {
		log.Fatal()
	}
}
