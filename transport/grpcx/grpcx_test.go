package grpcx_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/grpcx"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestClientDialAndHealthCheck(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpcx.NewServer(&grpcx.ServerConfig{
		Listener: listener,
	})

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Start(context.Background(), func(s *grpc.Server) {})
	}()

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)

		select {
		case err := <-serveDone:
			if err != nil {
				t.Fatalf("grpc server exited with error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("grpc server did not stop in time")
		}
	})

	client := grpcx.NewClient(&grpcx.ClientConfig{
		Addr: "passthrough:///bufnet",
	})
	client.AddDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))

	dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Second)
	defer dialCancel()

	conn, err := client.DialContext(dialCtx)
	if err != nil {
		t.Fatalf("DialContext() error = %v", err)
	}
	t.Cleanup(client.Close)

	healthClient := grpc_health_v1.NewHealthClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("HealthCheck() status = %v, want %v", resp.GetStatus(), grpc_health_v1.HealthCheckResponse_SERVING)
	}
}

func TestServerStartRejectsNilRegister(t *testing.T) {
	server := grpcx.NewServer(&grpcx.ServerConfig{
		Listener: bufconn.Listen(1024),
	})

	err := server.Start(context.Background(), nil)
	if err == nil {
		t.Fatal("expected Start to reject nil register func")
	}
}

func TestServerStartStopsWhenContextCanceled(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpcx.NewServer(&grpcx.ServerConfig{
		Listener: listener,
	})

	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Start(ctx, func(s *grpc.Server) {})
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if server.Engine() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if server.Engine() == nil {
		t.Fatal("server did not start in time")
	}

	cancel()

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Start() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func TestWaitForReadyRejectsNilConn(t *testing.T) {
	err := grpcx.WaitForReady(context.Background(), nil)
	if err == nil {
		t.Fatal("expected WaitForReady to reject nil connection")
	}
}

func TestWaitForReadyRejectsNilContext(t *testing.T) {
	var ctx context.Context

	err := grpcx.WaitForReady(ctx, &grpc.ClientConn{})
	if err == nil {
		t.Fatal("expected WaitForReady to reject nil context")
	}
}

func TestDialContextHonorsDeadline(t *testing.T) {
	client := grpcx.NewClient(&grpcx.ClientConfig{
		Addr: "passthrough:///unreachable",
	})
	client.AddDialOptions(grpc.WithContextDialer(func(ctx context.Context, target string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.DialContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DialContext() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestDialContextRejectsNilContext(t *testing.T) {
	client := grpcx.NewClient(&grpcx.ClientConfig{
		Addr: "passthrough:///unused",
	})

	var ctx context.Context
	_, err := client.DialContext(ctx)
	if err == nil {
		t.Fatal("expected DialContext to reject nil context")
	}
}

func TestNewClientRegistersMetricsWithCustomRegisterer(t *testing.T) {
	reg := prometheus.NewRegistry()

	_ = grpcx.NewClient(&grpcx.ClientConfig{
		Addr:              "passthrough:///unused",
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "grpc_client_request_duration_seconds",
			Help: "gRPC client request duration in seconds",
		},
		[]string{"method", "code"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_client_requests_total",
			Help: "gRPC client requests total",
		},
		[]string{"method", "code"},
	))
}

func TestNewServerRegistersMetricsWithCustomRegisterer(t *testing.T) {
	reg := prometheus.NewRegistry()

	_ = grpcx.NewServer(&grpcx.ServerConfig{
		Listener:          bufconn.Listen(1024),
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "grpc_server_request_duration_seconds",
			Help: "gRPC server request duration in seconds",
		},
		[]string{"method", "code"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_server_requests_total",
			Help: "gRPC server requests total",
		},
		[]string{"method", "code"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grpc_server_requests_in_flight",
			Help: "gRPC server requests currently processing",
		},
		[]string{"method"},
	))
}

func assertCollectorRegistered(t *testing.T, reg *prometheus.Registry, collector prometheus.Collector) {
	t.Helper()

	err := reg.Register(collector)
	if err == nil {
		t.Fatal("expected collector to already be registered")
	}
	if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
		t.Fatalf("Register() error = %v, want AlreadyRegisteredError", err)
	}
}
