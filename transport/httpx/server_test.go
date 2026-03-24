package httpx_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/micro/transport/httpx"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestServerStartShutdownAndRecovery(t *testing.T) {
	listener := newPipeListener()

	server := httpx.NewServer(&httpx.ServerConfig{
		Listener:        listener,
		ShutdownTimeout: 2 * time.Second,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/hello":
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("ok"))
			case "/panic":
				panic("boom")
			default:
				http.NotFound(w, r)
			}
		}))
	}()

	waitForServer(t, server)

	status, body, _, err := doPipeRequest(listener, http.MethodGet, "/hello")
	if err != nil {
		t.Fatalf("request /hello: %v", err)
	}
	if got, want := status, http.StatusAccepted; got != want {
		t.Fatalf("/hello status = %d, want %d", got, want)
	}
	if got, want := body, "ok"; got != want {
		t.Fatalf("/hello body = %q, want %q", got, want)
	}

	status, body, _, err = doPipeRequest(listener, http.MethodGet, "/panic")
	if err != nil {
		t.Fatalf("request /panic: %v", err)
	}
	if got, want := status, http.StatusInternalServerError; got != want {
		t.Fatalf("/panic status = %d, want %d", got, want)
	}
	if got, want := body, "Internal Server Error\n"; got != want {
		t.Fatalf("/panic body = %q, want %q", got, want)
	}

	status, body, _, err = doPipeRequest(listener, http.MethodGet, "/healthz")
	if err != nil {
		t.Fatalf("request /healthz: %v", err)
	}
	if got, want := status, http.StatusOK; got != want {
		t.Fatalf("/healthz status = %d, want %d", got, want)
	}
	if got, want := body, "OK"; got != want {
		t.Fatalf("/healthz body = %q, want %q", got, want)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerRejectsSecondStart(t *testing.T) {
	listener := newPipeListener()

	server := httpx.NewServer(&httpx.ServerConfig{Listener: listener})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
	}()

	waitForServer(t, server)

	err := server.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if !errors.Is(err, httpx.ErrServerAlreadyRunning) {
		t.Fatalf("second start error = %v, want %v", err, httpx.ErrServerAlreadyRunning)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerRecoveryDoesNotCorruptStartedResponseWithTracing(t *testing.T) {
	listener := newPipeListener()

	server := httpx.NewServer(&httpx.ServerConfig{
		Listener: listener,
		Trace:    true,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("partial"))
			panic("boom")
		}))
	}()

	waitForServer(t, server)

	status, body, _, err := doPipeRequest(listener, http.MethodGet, "/panic-after-write")
	if err != nil {
		t.Fatalf("request /panic-after-write: %v", err)
	}
	if got, want := status, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := body, "partial"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerValidation(t *testing.T) {
	server := httpx.NewServer(&httpx.ServerConfig{DisableHealthEndpoint: true})

	if err := server.Start(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); !errors.Is(err, httpx.ErrContextRequired) {
		t.Fatalf("nil start context error = %v, want %v", err, httpx.ErrContextRequired)
	}
	if err := server.Serve(nil, newPipeListener(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); !errors.Is(err, httpx.ErrContextRequired) {
		t.Fatalf("nil serve context error = %v, want %v", err, httpx.ErrContextRequired)
	}
	if err := server.Shutdown(nil); !errors.Is(err, httpx.ErrContextRequired) {
		t.Fatalf("nil shutdown context error = %v, want %v", err, httpx.ErrContextRequired)
	}
	if err := server.Start(context.Background(), nil); !errors.Is(err, httpx.ErrNilHandler) {
		t.Fatalf("nil handler error = %v, want %v", err, httpx.ErrNilHandler)
	}

	if err := server.Serve(context.Background(), nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); !errors.Is(err, httpx.ErrNilListener) {
		t.Fatalf("nil listener error = %v, want %v", err, httpx.ErrNilListener)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.Start(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start context error = %v, want %v", err, context.Canceled)
	}
	if err := server.Serve(ctx, newPipeListener(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled serve context error = %v, want %v", err, context.Canceled)
	}
}

func TestServerStopsWhenStartContextCanceled(t *testing.T) {
	listener := newPipeListener()
	server := httpx.NewServer(&httpx.ServerConfig{
		Listener: listener,
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
	}()

	waitForServer(t, server)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("start returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func TestServerShutdownWithCanceledContextForcesClose(t *testing.T) {
	listener := newPipeListener()
	server := httpx.NewServer(&httpx.ServerConfig{
		Listener: listener,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
	}()

	waitForServer(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := server.Shutdown(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown error = %v, want %v", err, context.Canceled)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestRequestContextInheritsServerContext(t *testing.T) {
	type contextKey string

	const key contextKey = "server-value"
	listener := newPipeListener()
	server := httpx.NewServer(&httpx.ServerConfig{
		Listener: listener,
	})

	errCh := make(chan error, 1)
	go func() {
		ctx := context.WithValue(context.Background(), key, "inherited")
		errCh <- server.Start(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value, _ := r.Context().Value(key).(string)
			_, _ = w.Write([]byte(value))
		}))
	}()

	waitForServer(t, server)

	status, body, _, err := doPipeRequest(listener, http.MethodGet, "/ctx")
	if err != nil {
		t.Fatalf("request /ctx: %v", err)
	}
	if got, want := status, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := body, "inherited"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestServerMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = httpx.NewServer(&httpx.ServerConfig{
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "httpx_server_request_duration_seconds",
			Help: "HTTP server request duration in seconds.",
		},
		[]string{"method", "code"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "httpx_server_requests_total",
			Help: "Total number of HTTP server requests.",
		},
		[]string{"method", "code"},
	))

	disabledReg := prometheus.NewRegistry()
	_ = httpx.NewServer(&httpx.ServerConfig{
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})

	assertCollectorNotRegistered(t, disabledReg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "httpx_server_request_duration_seconds",
			Help: "HTTP server request duration in seconds.",
		},
		[]string{"method", "code"},
	))
}

func TestDisableHealthEndpointHealthRouteRecordedInMetrics(t *testing.T) {
	listener := newPipeListener()
	reg := prometheus.NewRegistry()

	server := httpx.NewServer(&httpx.ServerConfig{
		Listener:              listener,
		DisableHealthEndpoint: true,
		MetricsRegisterer:     reg,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.NotFound(w, r)
		}))
	}()

	waitForServer(t, server)

	status, _, _, err := doPipeRequest(listener, http.MethodGet, "/healthz")
	if err != nil {
		t.Fatalf("request /healthz: %v", err)
	}
	if got, want := status, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	assertCounterValue(t, reg, "httpx_server_requests_total", map[string]string{
		"method": "GET",
		"code":   "204",
	}, 1)
}

func TestDisableHealthEndpointHealthRouteNotSkippedInAccessLog(t *testing.T) {
	listener := newPipeListener()
	var output bytes.Buffer
	log := logger.New(
		logger.WithOutput(&output),
		logger.WithFormat("json"),
		logger.WithAddSource(false),
	)

	server := httpx.NewServer(&httpx.ServerConfig{
		Listener:              listener,
		Logger:                log,
		EnableLogger:          true,
		DisableHealthEndpoint: true,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.NotFound(w, r)
		}))
	}()

	waitForServer(t, server)

	if _, _, _, err := doPipeRequest(listener, http.MethodGet, "/healthz"); err != nil {
		t.Fatalf("request /healthz: %v", err)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	logs := output.String()
	if got, want := strings.Count(logs, `"msg":"http_server_request"`), 1; got != want {
		t.Fatalf("http_server_request log count = %d, want %d\nlogs=%s", got, want, logs)
	}
	if !strings.Contains(logs, `"/healthz"`) {
		t.Fatalf("logs missing /healthz route: %s", logs)
	}
}

func waitForServer(t *testing.T, server httpx.Server) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if server.HTTPServer() != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
}

type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		conns:  make(chan net.Conn, 16),
		closed: make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	case conn := <-l.conns:
		return conn, nil
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return pipeAddr("pipe")
}

func (l *pipeListener) Dial() (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case <-l.closed:
		_ = client.Close()
		_ = server.Close()
		return nil, net.ErrClosed
	case l.conns <- server:
		return client, nil
	}
}

type pipeAddr string

func (p pipeAddr) Network() string { return string(p) }
func (p pipeAddr) String() string  { return string(p) }

func doPipeRequest(listener *pipeListener, method, path string) (int, string, http.Header, error) {
	conn, err := listener.Dial()
	if err != nil {
		return 0, "", nil, err
	}
	defer conn.Close()

	raw := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: micro.test\r\nConnection: close\r\n\r\n", method, path)
	if _, err := io.WriteString(conn, raw); err != nil {
		return 0, "", nil, err
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: method})
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", nil, err
	}
	return resp.StatusCode, string(body), resp.Header.Clone(), nil
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

func assertCounterValue(t *testing.T, reg *prometheus.Registry, metricName string, labels map[string]string, want float64) {
	t.Helper()

	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != metricName {
			continue
		}
		for _, metric := range metricFamily.GetMetric() {
			if labelsMatch(metric, labels) {
				if got := metric.GetCounter().GetValue(); got != want {
					t.Fatalf("%s labels=%v value = %v, want %v", metricName, labels, got, want)
				}
				return
			}
		}
		t.Fatalf("metric %s missing labels %v", metricName, labels)
	}
	t.Fatalf("metric %s not found", metricName)
}

func labelsMatch(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}
	for _, label := range metric.GetLabel() {
		if labels[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}
