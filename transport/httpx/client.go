package httpx

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/opt"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	ContentRaw  = "Raw"  //原始请求
	ContentForm = "Form" //Form请求
	ContentJson = "Json" //Json请求
)

const (
	MethodGet     = http.MethodGet
	MethodHead    = http.MethodHead
	MethodPost    = http.MethodPost
	MethodPut     = http.MethodPut
	MethodPatch   = http.MethodPatch // RFC 5789
	MethodDelete  = http.MethodDelete
	MethodConnect = http.MethodConnect
	MethodOptions = http.MethodOptions
	MethodTrace   = http.MethodTrace
)

// Prometheus Metrics
var (
	ClientRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "httpx_client_request_duration_seconds",
			Help:    "HTTP client request duration in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "code", "host"},
	)

	ClientRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "httpx_client_requests_total",
			Help: "HTTP client requests total",
		},
		[]string{"method", "code", "host"},
	)
)

func init() {
	prometheus.MustRegister(ClientRequestDuration)
	prometheus.MustRegister(ClientRequestsTotal)
}

type Client interface {
	Send(ctx context.Context, req *Request, opts ...opt.Option[requestOptions]) (resp *Response, err error)
}

type clientEntity struct {
	config     *Config
	httpClient *http.Client
}

func New(conf *Config) Client {
	if conf == nil {
		conf = &Config{}
	}
	if conf.Logger == nil && conf.EnableLogger {
		conf.Logger = logger.New(logger.WithLevel("info"))
	}

	var transport *http.Transport
	if conf.Transport != nil {
		transport = conf.Transport
	} else {
		// Default Transport with Connection Pool Optimization
		transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConnsPerHost:   10, // Default in net/http is 2, increased for high throughput
		}

		// Apply user configs
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
	}

	timeout := 30 * time.Second
	if conf.Timeout > 0 {
		timeout = conf.Timeout
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	if conf.Trace {
		httpClient.Transport = otelhttp.NewTransport(transport,
			otelhttp.WithFilter(func(r *http.Request) bool {
				for _, p := range conf.ObservabilitySkipPaths {
					if r.URL.Path == p {
						return false
					}
				}
				return true
			}),
		)
	}

	return &clientEntity{
		config:     conf,
		httpClient: httpClient,
	}
}

func (c clientEntity) Send(ctx context.Context, req *Request, opts ...opt.Option[requestOptions]) (resp *Response, err error) {
	options := &requestOptions{}
	opt.Each(options, opts...)
	httpUrl, err := req.getUrl()
	if err != nil {
		return
	}
	method, err := req.getMethod()
	if err != nil {
		return
	}
	reqBody := req.getBody()
	var httpReq *http.Request
	var httpRes *http.Response
	if httpReq, err = http.NewRequestWithContext(ctx, method, httpUrl, reqBody); err != nil { //新建http请求
		return
	}
	req.setHeaders(httpReq) //init headers
	//basic auth
	if options.baseAuth != nil {
		httpReq.SetBasicAuth(options.baseAuth.Username, options.baseAuth.Password)
	}
	req.setCookie(httpReq) ////init cookie

	startTime := time.Now()
	// Retry logic could be added here
	if httpRes, err = c.httpClient.Do(httpReq); err != nil {
		// Log error
		if c.config.EnableLogger {
			c.config.Logger.Error(ctx, "http_client_request_failed",
				"method", method,
				"url", httpUrl,
				"error", err,
				"cost", time.Since(startTime).Seconds(),
			)
		}
		return
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpRes.Body)
	endTime := time.Now()
	elapsed := endTime.Sub(startTime).Seconds()
	resp = req.packResponse(httpRes, elapsed)

	// Observability
	host := httpReq.URL.Host
	code := httpRes.StatusCode

	// Metrics
	// Check skip paths
	shouldRecordMetric := true
	for _, p := range c.config.ObservabilitySkipPaths {
		if httpReq.URL.Path == p {
			shouldRecordMetric = false
			break
		}
	}

	if shouldRecordMetric {
		ClientRequestDuration.WithLabelValues(method, http.StatusText(code), host).Observe(elapsed)
		ClientRequestsTotal.WithLabelValues(method, http.StatusText(code), host).Inc()
	}

	// Logging
	if c.config.EnableLogger {
		c.config.Logger.Info(ctx, "http_client_access_log",
			"method", method,
			"url", httpUrl,
			"status", code,
			"duration", elapsed,
		)
	}

	return
}
