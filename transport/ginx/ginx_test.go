package ginx_test

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
	"github.com/bang-go/micro/transport/ginx"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestServerStartShutdownAndRecovery(t *testing.T) {
	listener := newPipeListener()

	server := ginx.New(&ginx.ServerConfig{
		Listener:        listener,
		Mode:            gin.TestMode,
		ShutdownTimeout: 2 * time.Second,
	})

	router := server.GinEngine()
	router.GET("/hello", func(c *gin.Context) {
		c.String(http.StatusAccepted, "ok")
	})
	router.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
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
	if got, want := body, ""; got != want {
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

	server := ginx.New(&ginx.ServerConfig{
		Listener: listener,
		Mode:     gin.TestMode,
	})
	server.GinEngine().GET("/hello", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
	}()

	waitForServer(t, server)

	err := server.Start(context.Background())
	if !errors.Is(err, ginx.ErrServerAlreadyRunning) {
		t.Fatalf("second start error = %v, want %v", err, ginx.ErrServerAlreadyRunning)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestDisableHealthEndpointAllowsCustomRoute(t *testing.T) {
	listener := newPipeListener()

	server := ginx.New(&ginx.ServerConfig{
		Listener:              listener,
		Mode:                  gin.TestMode,
		DisableHealthEndpoint: true,
	})
	server.GinEngine().GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusCreated, "custom")
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
	}()

	waitForServer(t, server)

	status, body, _, err := doPipeRequest(listener, http.MethodGet, "/healthz")
	if err != nil {
		t.Fatalf("request /healthz: %v", err)
	}
	if got, want := status, http.StatusCreated; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := body, "custom"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestDisableHealthEndpointDoesNotSkipObservability(t *testing.T) {
	listener := newPipeListener()

	var output bytes.Buffer
	log := logger.New(
		logger.WithOutput(&output),
		logger.WithFormat("json"),
		logger.WithAddSource(false),
	)

	server := ginx.New(&ginx.ServerConfig{
		Listener:              listener,
		Mode:                  gin.TestMode,
		Logger:                log,
		EnableLogger:          true,
		DisableHealthEndpoint: true,
	})
	server.GinEngine().GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
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

	logs := output.String()
	if got, want := strings.Count(logs, `"msg":"http_server_request"`), 1; got != want {
		t.Fatalf("http_server_request log count = %d, want %d\nlogs=%s", got, want, logs)
	}
	if !strings.Contains(logs, `"/healthz"`) {
		t.Fatalf("logs missing /healthz route: %s", logs)
	}
}

func TestServerAccessLogSkipsHealthAndDoesNotLeakQuery(t *testing.T) {
	listener := newPipeListener()

	var output bytes.Buffer
	log := logger.New(
		logger.WithOutput(&output),
		logger.WithFormat("json"),
		logger.WithAddSource(false),
	)

	server := ginx.New(&ginx.ServerConfig{
		Listener:     listener,
		Mode:         gin.TestMode,
		Logger:       log,
		EnableLogger: true,
	})
	server.GinEngine().GET("/items/:id", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
	}()

	waitForServer(t, server)

	if _, _, _, err := doPipeRequest(listener, http.MethodGet, "/healthz"); err != nil {
		t.Fatalf("request /healthz: %v", err)
	}
	if _, _, _, err := doPipeRequest(listener, http.MethodGet, "/items/42?token=secret"); err != nil {
		t.Fatalf("request /items: %v", err)
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
	if !strings.Contains(logs, `"/items/42"`) {
		t.Fatalf("logs missing request path: %s", logs)
	}
	if !strings.Contains(logs, `"/items/:id"`) {
		t.Fatalf("logs missing route label: %s", logs)
	}
	if strings.Contains(logs, "token=secret") {
		t.Fatalf("logs leaked query string: %s", logs)
	}
	if strings.Contains(logs, `"/healthz"`) {
		t.Fatalf("logs should skip health endpoint: %s", logs)
	}
}

func TestServerRecoveryDoesNotOverwriteStartedResponseWithTrace(t *testing.T) {
	listener := newPipeListener()
	var output bytes.Buffer
	log := logger.New(
		logger.WithOutput(&output),
		logger.WithFormat("json"),
		logger.WithAddSource(false),
	)

	server := ginx.New(&ginx.ServerConfig{
		Listener:     listener,
		Mode:         gin.TestMode,
		Trace:        true,
		Logger:       log,
		EnableLogger: true,
	})
	server.GinEngine().GET("/partial", func(c *gin.Context) {
		_, _ = c.Writer.Write([]byte("partial"))
		panic("boom")
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
	}()

	waitForServer(t, server)

	status, body, _, err := doPipeRequest(listener, http.MethodGet, "/partial")
	if err != nil {
		t.Fatalf("request /partial: %v", err)
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

	logs := output.String()
	if got, want := strings.Count(logs, `"msg":"http_server_panic_recovered"`), 1; got != want {
		t.Fatalf("panic recovery log count = %d, want %d\nlogs=%s", got, want, logs)
	}
	if got, want := strings.Count(logs, `"msg":"http_server_request"`), 1; got != want {
		t.Fatalf("access log count = %d, want %d\nlogs=%s", got, want, logs)
	}
	if got, want := strings.Count(logs, `"level":"ERROR"`), 1; got != want {
		t.Fatalf("error log count = %d, want %d\nlogs=%s", got, want, logs)
	}
	if got, want := strings.Count(logs, `"level":"WARN"`), 1; got != want {
		t.Fatalf("warn log count = %d, want %d\nlogs=%s", got, want, logs)
	}
	if !strings.Contains(logs, "boom") {
		t.Fatalf("access log should include panic error\nlogs=%s", logs)
	}
}

func TestServerValidation(t *testing.T) {
	server := ginx.New(&ginx.ServerConfig{
		Mode:                  gin.TestMode,
		DisableHealthEndpoint: true,
	})

	if err := server.Serve(context.Background(), nil); !errors.Is(err, ginx.ErrNilListener) {
		t.Fatalf("nil listener error = %v, want %v", err, ginx.ErrNilListener)
	}
}

func TestServerRejectsNilContext(t *testing.T) {
	listener := newPipeListener()
	server := ginx.New(&ginx.ServerConfig{
		Listener: listener,
		Mode:     gin.TestMode,
	})

	var ctx context.Context
	if err := server.Start(ctx); !errors.Is(err, ginx.ErrContextRequired) {
		t.Fatalf("Start(nil) error = %v, want %v", err, ginx.ErrContextRequired)
	}
	if err := server.Serve(ctx, listener); !errors.Is(err, ginx.ErrContextRequired) {
		t.Fatalf("Serve(nil) error = %v, want %v", err, ginx.ErrContextRequired)
	}
	if err := server.Shutdown(ctx); !errors.Is(err, ginx.ErrContextRequired) {
		t.Fatalf("Shutdown(nil) error = %v, want %v", err, ginx.ErrContextRequired)
	}
}

func TestServerRejectsCanceledStartContext(t *testing.T) {
	listener := newPipeListener()
	server := ginx.New(&ginx.ServerConfig{
		Listener: listener,
		Mode:     gin.TestMode,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := server.Start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start(canceled) error = %v, want %v", err, context.Canceled)
	}
	if server.HTTPServer() != nil {
		t.Fatal("server should not have started")
	}
}

func TestServerStopsWhenStartContextCanceled(t *testing.T) {
	listener := newPipeListener()
	server := ginx.New(&ginx.ServerConfig{
		Listener: listener,
		Mode:     gin.TestMode,
	})
	server.GinEngine().GET("/hello", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	waitForServer(t, server)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func TestServerShutdownWithCanceledContextForcesClose(t *testing.T) {
	listener := newPipeListener()
	server := ginx.New(&ginx.ServerConfig{
		Listener: listener,
		Mode:     gin.TestMode,
	})
	server.GinEngine().GET("/hello", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
	}()

	waitForServer(t, server)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := server.Shutdown(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown(canceled) error = %v, want %v", err, context.Canceled)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("start returned error: %v", err)
	}
}

func TestRequestContextInheritsServerContext(t *testing.T) {
	type contextKey string

	const key contextKey = "server-value"
	listener := newPipeListener()
	server := ginx.New(&ginx.ServerConfig{
		Listener: listener,
		Mode:     gin.TestMode,
	})
	server.GinEngine().GET("/ctx", func(c *gin.Context) {
		value, _ := c.Request.Context().Value(key).(string)
		c.String(http.StatusOK, value)
	})

	ctx := context.WithValue(context.Background(), key, "inherited")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
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

func TestMetricsRegistererAndDisableMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = ginx.New(&ginx.ServerConfig{
		Mode:              gin.TestMode,
		DisableMetrics:    false,
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "ginx_server_request_duration_seconds",
			Help: "Gin HTTP server request duration in seconds.",
		},
		[]string{"method", "route", "status"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ginx_server_requests_total",
			Help: "Total number of Gin HTTP server requests.",
		},
		[]string{"method", "route", "status"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ginx_server_requests_in_flight",
			Help: "Current number of in-flight Gin HTTP server requests.",
		},
		[]string{"method", "route"},
	))

	disabledReg := prometheus.NewRegistry()
	_ = ginx.New(&ginx.ServerConfig{
		Mode:              gin.TestMode,
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})

	assertCollectorNotRegistered(t, disabledReg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "ginx_server_request_duration_seconds",
			Help: "Gin HTTP server request duration in seconds.",
		},
		[]string{"method", "route", "status"},
	))
}

func TestDisableHealthEndpointHealthRouteRecordedInMetrics(t *testing.T) {
	listener := newPipeListener()
	reg := prometheus.NewRegistry()

	server := ginx.New(&ginx.ServerConfig{
		Listener:              listener,
		Mode:                  gin.TestMode,
		DisableHealthEndpoint: true,
		MetricsRegisterer:     reg,
	})
	server.GinEngine().GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(context.Background())
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

	assertCounterValue(t, reg, "ginx_server_requests_total", map[string]string{
		"method": "GET",
		"route":  "/healthz",
		"status": "204",
	}, 1)
}

func waitForServer(t *testing.T, server ginx.Server) {
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

func doPipeRequest(listener *pipeListener, method, target string) (int, string, http.Header, error) {
	conn, err := listener.Dial()
	if err != nil {
		return 0, "", nil, err
	}
	defer conn.Close()

	raw := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: micro.test\r\nConnection: close\r\n\r\n", method, target)
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

func assertCounterValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string, want float64) {
	t.Helper()

	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	for _, family := range metricFamilies {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric, labels) {
				if got := metric.GetCounter().GetValue(); got != want {
					t.Fatalf("%s value = %v, want %v", name, got, want)
				}
				return
			}
		}
	}

	t.Fatalf("metric %s with labels %v not found", name, labels)
}

func labelsMatch(metric *dto.Metric, want map[string]string) bool {
	if len(metric.GetLabel()) != len(want) {
		return false
	}
	for _, label := range metric.GetLabel() {
		if want[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}
