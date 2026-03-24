package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	ContentTypeJSON              = "application/json"
	ContentTypeFormURLEncoded    = "application/x-www-form-urlencoded"
	ContentTypeTextPlain         = "text/plain; charset=utf-8"
	ContentTypeOctetStream       = "application/octet-stream"
	defaultClientTimeout         = 30 * time.Second
	defaultDialTimeout           = 30 * time.Second
	defaultDialKeepAlive         = 30 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 100
	defaultMaxIdleConnsPerHost   = 10
)

const (
	MethodGet     = http.MethodGet
	MethodHead    = http.MethodHead
	MethodPost    = http.MethodPost
	MethodPut     = http.MethodPut
	MethodPatch   = http.MethodPatch
	MethodDelete  = http.MethodDelete
	MethodConnect = http.MethodConnect
	MethodOptions = http.MethodOptions
	MethodTrace   = http.MethodTrace
)

type Client interface {
	Do(context.Context, *Request) (*Response, error)
	HTTPClient() *http.Client
	CloseIdleConnections()
}

type clientEntity struct {
	config                 *ClientConfig
	httpClient             *http.Client
	closeIdleConnectionsFn func()
	skipPaths              map[string]struct{}
	metrics                *metrics
}

func NewClient(conf *ClientConfig) Client {
	if conf == nil {
		conf = &ClientConfig{}
	}
	if conf.Logger == nil && conf.EnableLogger {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	var metrics *metrics
	if !conf.DisableMetrics {
		metrics = defaultHTTPMetrics()
		if conf.MetricsRegisterer != nil {
			metrics = newHTTPMetrics(conf.MetricsRegisterer)
		}
	}

	httpClient := newBaseHTTPClient(conf)
	skipPaths := newSkipPathSet(nil, conf.ObservabilitySkipPaths)
	transport, closeIdleConnectionsFn := buildClientTransport(conf, httpClient.Transport)
	if conf.Trace {
		httpClient.Transport = otelhttp.NewTransport(transport, otelhttp.WithFilter(func(r *http.Request) bool {
			return !matchesPath(skipPaths, r.URL.Path)
		}))
	} else {
		httpClient.Transport = transport
	}

	return &clientEntity{
		config:                 conf,
		httpClient:             httpClient,
		closeIdleConnectionsFn: closeIdleConnectionsFn,
		skipPaths:              skipPaths,
		metrics:                metrics,
	}
}

func (c *clientEntity) Do(ctx context.Context, req *Request) (*Response, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	httpReq, err := req.Build(ctx)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	httpResp, err := c.httpClient.Do(httpReq)
	duration := time.Since(start)
	if err != nil {
		c.record(httpReq, 0, duration, err)
		return nil, err
	}

	body, readErr := io.ReadAll(httpResp.Body)
	closeErr := httpResp.Body.Close()
	effectiveReq := effectiveRequest(httpReq, httpResp)
	resp := newResponse(effectiveReq, httpResp, body, duration)
	c.record(effectiveReq, httpResp.StatusCode, duration, errors.Join(readErr, closeErr))

	if readErr != nil || closeErr != nil {
		return resp, errors.Join(readErr, closeErr)
	}
	return resp, nil
}

func (c *clientEntity) HTTPClient() *http.Client {
	return c.httpClient
}

func (c *clientEntity) CloseIdleConnections() {
	if c.closeIdleConnectionsFn != nil {
		c.closeIdleConnectionsFn()
	}
}

func (c *clientEntity) record(req *http.Request, statusCode int, duration time.Duration, err error) {
	if matchesPath(c.skipPaths, req.URL.Path) {
		return
	}

	code := statusLabel(statusCode)
	if c.metrics != nil {
		c.metrics.clientRequestDuration.WithLabelValues(req.Method, code).Observe(duration.Seconds())
		c.metrics.clientRequestsTotal.WithLabelValues(req.Method, code).Inc()
	}

	if !c.config.EnableLogger || c.config.Logger == nil {
		return
	}

	if err != nil {
		c.config.Logger.Error(req.Context(), "http_client_request_failed",
			"method", req.Method,
			"url", redactedURLString(req.URL),
			"status", statusCode,
			"duration", duration.Seconds(),
			"error", err,
		)
		return
	}

	if statusCode >= http.StatusInternalServerError {
		c.config.Logger.Error(req.Context(), "http_client_request",
			"method", req.Method,
			"url", redactedURLString(req.URL),
			"status", statusCode,
			"duration", duration.Seconds(),
		)
		return
	}
	if statusCode >= http.StatusBadRequest {
		c.config.Logger.Warn(req.Context(), "http_client_request",
			"method", req.Method,
			"url", redactedURLString(req.URL),
			"status", statusCode,
			"duration", duration.Seconds(),
		)
		return
	}

	c.config.Logger.Info(req.Context(), "http_client_request",
		"method", req.Method,
		"url", redactedURLString(req.URL),
		"status", statusCode,
		"duration", duration.Seconds(),
	)
}

func newBaseHTTPClient(conf *ClientConfig) *http.Client {
	if conf.HTTPClient != nil {
		cloned := *conf.HTTPClient
		if conf.Timeout > 0 {
			cloned.Timeout = conf.Timeout
		} else if cloned.Timeout == 0 {
			cloned.Timeout = defaultClientTimeout
		}
		if conf.CheckRedirect != nil {
			cloned.CheckRedirect = conf.CheckRedirect
		}
		if conf.Jar != nil {
			cloned.Jar = conf.Jar
		}
		return &cloned
	}

	httpClient := &http.Client{
		Timeout: defaultClientTimeout,
	}
	if conf.Timeout > 0 {
		httpClient.Timeout = conf.Timeout
	}
	if conf.CheckRedirect != nil {
		httpClient.CheckRedirect = conf.CheckRedirect
	}
	if conf.Jar != nil {
		httpClient.Jar = conf.Jar
	}
	return httpClient
}

func buildClientTransport(conf *ClientConfig, existing http.RoundTripper) (http.RoundTripper, func()) {
	if conf.Transport != nil {
		transport := conf.Transport.Clone()
		applyClientTransportConfig(conf, transport)
		return transport, transport.CloseIdleConnections
	}

	if transport, ok := existing.(*http.Transport); ok && transport != nil {
		cloned := transport.Clone()
		applyClientTransportConfig(conf, cloned)
		return cloned, cloned.CloseIdleConnections
	}

	if existing != nil {
		var closeIdleConnectionsFn func()
		if closer, ok := existing.(interface{ CloseIdleConnections() }); ok {
			closeIdleConnectionsFn = closer.CloseIdleConnections
		}
		return existing, closeIdleConnectionsFn
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: defaultDialKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          defaultMaxIdleConns,
		MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ExpectContinueTimeout: defaultExpectContinueTimeout,
	}
	applyClientTransportConfig(conf, transport)
	return transport, transport.CloseIdleConnections
}

func applyClientTransportConfig(conf *ClientConfig, transport *http.Transport) {
	if conf.MaxIdleConns > 0 {
		transport.MaxIdleConns = conf.MaxIdleConns
	}
	if conf.MaxIdleConnsPerHost > 0 {
		transport.MaxIdleConnsPerHost = conf.MaxIdleConnsPerHost
	}
	if conf.MaxConnsPerHost > 0 {
		transport.MaxConnsPerHost = conf.MaxConnsPerHost
	}
	if conf.IdleConnTimeout > 0 {
		transport.IdleConnTimeout = conf.IdleConnTimeout
	}
	if conf.TLSHandshakeTimeout > 0 {
		transport.TLSHandshakeTimeout = conf.TLSHandshakeTimeout
	}
	if conf.ResponseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = conf.ResponseHeaderTimeout
	}
	if conf.ExpectContinueTimeout > 0 {
		transport.ExpectContinueTimeout = conf.ExpectContinueTimeout
	}
}
