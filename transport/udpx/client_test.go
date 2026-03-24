package udpx_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/udpx"
	"github.com/prometheus/client_golang/prometheus"
)

func TestClientDialContextAndDatagramIO(t *testing.T) {
	raw := newFakeDatagram()
	raw.remoteAddr = testAddr("127.0.0.1:40001")
	raw.enqueueRead([]byte("pong"), raw.remoteAddr)

	client := udpx.NewClient(&udpx.ClientConfig{
		Addr:         "127.0.0.1:40001",
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		ContextDialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if got, want := network, "udp"; got != want {
				t.Fatalf("network = %q, want %q", got, want)
			}
			if got, want := addr, "127.0.0.1:40001"; got != want {
				t.Fatalf("addr = %q, want %q", got, want)
			}
			return raw, nil
		},
	})

	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, want := string(raw.writes[0]), "ping"; got != want {
		t.Fatalf("written = %q, want %q", got, want)
	}

	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got, want := string(buf[:n]), "pong"; got != want {
		t.Fatalf("read = %q, want %q", got, want)
	}
}

func TestClientDialContextCanceled(t *testing.T) {
	client := udpx.NewClient(&udpx.ClientConfig{
		Addr: "127.0.0.1:40002",
		ContextDialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			t.Fatal("dialer should not be invoked when context is already canceled")
			return nil, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.DialContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dial error = %v, want %v", err, context.Canceled)
	}
}

func TestClientDialContextRequiresContext(t *testing.T) {
	client := udpx.NewClient(&udpx.ClientConfig{
		Addr: "127.0.0.1:40003",
	})

	_, err := client.DialContext(nil)
	if !errors.Is(err, udpx.ErrContextRequired) {
		t.Fatalf("dial error = %v, want %v", err, udpx.ErrContextRequired)
	}
}

func TestClientMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = udpx.NewClient(&udpx.ClientConfig{
		Addr:              "127.0.0.1:40004",
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "udpx_client_dial_duration_seconds",
			Help: "UDP client dial duration in seconds.",
		},
		[]string{"result"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "udpx_client_dials_total",
			Help: "Total number of UDP client dial attempts.",
		},
		[]string{"result"},
	))

	disabledReg := prometheus.NewRegistry()
	_ = udpx.NewClient(&udpx.ClientConfig{
		Addr:              "127.0.0.1:40005",
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})

	assertCollectorNotRegistered(t, disabledReg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "udpx_client_dial_duration_seconds",
			Help: "UDP client dial duration in seconds.",
		},
		[]string{"result"},
	))
}
