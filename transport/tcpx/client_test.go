package tcpx_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/tcpx"
	"github.com/prometheus/client_golang/prometheus"
)

func TestClientDialContextAndIO(t *testing.T) {
	serverErrCh := make(chan error, 1)

	client := tcpx.NewClient(&tcpx.ClientConfig{
		Addr:         "pipe",
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		ContextDialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()

				buf := make([]byte, 4)
				if _, err := io.ReadFull(serverConn, buf); err != nil {
					serverErrCh <- err
					return
				}
				if string(buf) != "ping" {
					serverErrCh <- errors.New("unexpected payload")
					return
				}
				_, err := serverConn.Write([]byte("pong"))
				serverErrCh <- err
			}()
			return clientConn, nil
		},
	})

	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteFull([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	reply := make([]byte, 4)
	if err := conn.ReadFull(reply); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got, want := string(reply), "pong"; got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}

	if err := <-serverErrCh; err != nil {
		t.Fatalf("server err: %v", err)
	}
}

func TestClientDialContextCanceled(t *testing.T) {
	client := tcpx.NewClient(&tcpx.ClientConfig{
		Addr: "pipe",
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

func TestClientDialContextRejectsNilContext(t *testing.T) {
	client := tcpx.NewClient(&tcpx.ClientConfig{Addr: "pipe"})

	var ctx context.Context
	_, err := client.DialContext(ctx)
	if !errors.Is(err, tcpx.ErrContextRequired) {
		t.Fatalf("DialContext(nil) error = %v, want %v", err, tcpx.ErrContextRequired)
	}
}

func TestClientContextDialerWithTLSInfersServerName(t *testing.T) {
	certPEM, keyPEM := generateLocalhostCertificate(t)

	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair() error = %v", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("AppendCertsFromPEM() = false")
	}

	serverErrCh := make(chan error, 1)
	client := tcpx.NewClient(&tcpx.ClientConfig{
		Addr: "localhost:443",
		TLSConfig: &tls.Config{
			RootCAs: roots,
		},
		ContextDialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				tlsServer := tls.Server(serverConn, &tls.Config{
					Certificates: []tls.Certificate{serverCert},
				})
				serverErrCh <- tlsServer.HandshakeContext(ctx)
				_ = tlsServer.Close()
			}()
			return clientConn, nil
		},
	})

	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext() error = %v", err)
	}
	defer conn.Close()

	if err := <-serverErrCh; err != nil {
		t.Fatalf("server handshake error = %v", err)
	}
}

func TestClientMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = tcpx.NewClient(&tcpx.ClientConfig{
		Addr:              "pipe",
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "tcpx_client_dial_duration_seconds",
			Help: "TCP client dial duration in seconds.",
		},
		[]string{"result"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tcpx_client_dials_total",
			Help: "Total number of TCP client dial attempts.",
		},
		[]string{"result"},
	))

	disabledReg := prometheus.NewRegistry()
	_ = tcpx.NewClient(&tcpx.ClientConfig{
		Addr:              "pipe",
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})

	assertCollectorNotRegistered(t, disabledReg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "tcpx_client_dial_duration_seconds",
			Help: "TCP client dial duration in seconds.",
		},
		[]string{"result"},
	))
}

func generateLocalhostCertificate(t *testing.T) ([]byte, []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	return certPEM, keyPEM
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

func assertCollectorNotRegistered(t *testing.T, reg *prometheus.Registry, collector prometheus.Collector) {
	t.Helper()

	if err := reg.Register(collector); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
}
