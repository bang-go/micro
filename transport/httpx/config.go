package httpx

import (
	"net"
	"net/http"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/prometheus/client_golang/prometheus"
)

type ClientConfig struct {
	Logger       *logger.Logger
	EnableLogger bool
	Trace        bool

	Timeout time.Duration

	HTTPClient *http.Client
	Transport  *http.Transport

	CheckRedirect func(*http.Request, []*http.Request) error
	Jar           http.CookieJar

	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	ExpectContinueTimeout time.Duration

	// ObservabilitySkipPaths skips metrics, tracing, and access logging for
	// matching request paths. Client side defaults to none.
	ObservabilitySkipPaths []string
	MetricsRegisterer      prometheus.Registerer
	DisableMetrics         bool
}

type ServerConfig struct {
	Addr     string
	Listener net.Listener

	Logger       *logger.Logger
	EnableLogger bool
	Trace        bool

	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	// ObservabilitySkipPaths skips metrics, tracing, and access logging for
	// matching request paths. /metrics is always skipped; the default health
	// path is skipped only when the framework-managed health endpoint is enabled.
	ObservabilitySkipPaths []string
	MetricsRegisterer      prometheus.Registerer
	DisableMetrics         bool

	DisableHealthEndpoint bool
	HealthPath            string
	HealthHandler         http.Handler
}
