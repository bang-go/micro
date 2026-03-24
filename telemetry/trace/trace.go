package trace

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/bang-go/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

const (
	ExporterStdout   = "stdout"
	ExporterOTLP     = "otlp"
	ExporterOTLPHTTP = "otlphttp"
	ExporterOTLPGRPC = "otlpgrpc"

	CompressionGzip = "gzip"
)

var (
	ErrContextRequired     = errors.New("trace: context is required")
	ErrEndpointRequired    = errors.New("trace: endpoint is required for otlp exporters")
	ErrUnsupportedExporter = errors.New("trace: unsupported exporter")
	ErrInvalidSampleRate   = errors.New("trace: sample rate must be between 0 and 1")
)

type Config struct {
	ServiceName       string
	ServiceInstanceID string
	Endpoint          string
	Exporter          string
	SampleRate        *float64
	Headers           map[string]string
	Compression       string
	Timeout           time.Duration
	Insecure          bool
	PrettyPrint       bool

	newStdoutExporter func(*Config) (sdktrace.SpanExporter, error)
	newHTTPExporter   func(context.Context, httpExporterSettings) (sdktrace.SpanExporter, error)
	newGRPCExporter   func(context.Context, grpcExporterSettings) (sdktrace.SpanExporter, error)
}

type httpExporterSettings struct {
	Endpoint    string
	EndpointURL string
	Headers     map[string]string
	Compression string
	Timeout     time.Duration
	Insecure    bool
}

type grpcExporterSettings struct {
	Endpoint    string
	EndpointURL string
	Headers     map[string]string
	Compression string
	Timeout     time.Duration
	Insecure    bool
}

func Open(ctx context.Context, conf *Config) (*sdktrace.TracerProvider, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	config, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	exporter, err := buildExporter(ctx, config)
	if err != nil {
		return nil, err
	}

	resource, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceInstanceIDKey.String(config.ServiceInstanceID),
		),
	)
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource),
		sdktrace.WithSampler(buildSampler(*config.SampleRate)),
	)

	return tp, nil
}

func InitTracer(ctx context.Context, conf *Config) (func(context.Context) error, error) {
	tp, err := Open(ctx, conf)
	if err != nil {
		return nil, err
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func prepareConfig(conf *Config) (*Config, error) {
	if conf == nil {
		conf = &Config{}
	}

	cloned := *conf
	cloned.ServiceName = strings.TrimSpace(cloned.ServiceName)
	cloned.ServiceInstanceID = strings.TrimSpace(cloned.ServiceInstanceID)
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.Exporter = strings.ToLower(strings.TrimSpace(cloned.Exporter))
	cloned.Compression = strings.ToLower(strings.TrimSpace(cloned.Compression))
	cloned.Headers = cloneMap(cloned.Headers)

	if cloned.ServiceName == "" {
		cloned.ServiceName = "unknown-service"
	}
	if cloned.ServiceInstanceID == "" {
		cloned.ServiceInstanceID = defaultServiceInstanceID()
	}
	if cloned.Exporter == "" {
		cloned.Exporter = ExporterStdout
	}
	if cloned.SampleRate == nil {
		cloned.SampleRate = util.Ptr(1.0)
	} else if *cloned.SampleRate < 0 || *cloned.SampleRate > 1 {
		return nil, ErrInvalidSampleRate
	} else {
		cloned.SampleRate = util.ClonePtr(cloned.SampleRate)
	}

	if cloned.newStdoutExporter == nil {
		cloned.newStdoutExporter = func(cfg *Config) (sdktrace.SpanExporter, error) {
			options := []stdouttrace.Option{}
			if cfg.PrettyPrint {
				options = append(options, stdouttrace.WithPrettyPrint())
			}
			return stdouttrace.New(options...)
		}
	}
	if cloned.newHTTPExporter == nil {
		cloned.newHTTPExporter = defaultHTTPExporter
	}
	if cloned.newGRPCExporter == nil {
		cloned.newGRPCExporter = defaultGRPCExporter
	}

	return &cloned, nil
}

func buildExporter(ctx context.Context, conf *Config) (sdktrace.SpanExporter, error) {
	kind, err := exporterKind(conf)
	if err != nil {
		return nil, err
	}

	switch kind {
	case ExporterStdout:
		return conf.newStdoutExporter(conf)
	case ExporterOTLPHTTP:
		settings, err := buildHTTPExporterSettings(conf)
		if err != nil {
			return nil, err
		}
		return conf.newHTTPExporter(ctx, settings)
	case ExporterOTLPGRPC:
		settings, err := buildGRPCExporterSettings(conf)
		if err != nil {
			return nil, err
		}
		return conf.newGRPCExporter(ctx, settings)
	default:
		return nil, ErrUnsupportedExporter
	}
}

func exporterKind(conf *Config) (string, error) {
	switch conf.Exporter {
	case ExporterStdout:
		return ExporterStdout, nil
	case ExporterOTLPHTTP:
		if conf.Endpoint == "" {
			return "", ErrEndpointRequired
		}
		return ExporterOTLPHTTP, nil
	case ExporterOTLPGRPC:
		if conf.Endpoint == "" {
			return "", ErrEndpointRequired
		}
		return ExporterOTLPGRPC, nil
	case ExporterOTLP:
		if conf.Endpoint == "" {
			return "", ErrEndpointRequired
		}
		if hasHTTPScheme(conf.Endpoint) {
			return ExporterOTLPHTTP, nil
		}
		return ExporterOTLPGRPC, nil
	default:
		return "", ErrUnsupportedExporter
	}
}

func buildHTTPExporterSettings(conf *Config) (httpExporterSettings, error) {
	if conf.Endpoint == "" {
		return httpExporterSettings{}, ErrEndpointRequired
	}

	settings := httpExporterSettings{
		Headers:     cloneMap(conf.Headers),
		Compression: conf.Compression,
		Timeout:     conf.Timeout,
	}

	if hasHTTPScheme(conf.Endpoint) {
		settings.EndpointURL = conf.Endpoint
		settings.Insecure = strings.HasPrefix(strings.ToLower(conf.Endpoint), "http://")
	} else {
		settings.Endpoint = conf.Endpoint
		settings.Insecure = conf.Insecure
	}

	return settings, nil
}

func buildGRPCExporterSettings(conf *Config) (grpcExporterSettings, error) {
	if conf.Endpoint == "" {
		return grpcExporterSettings{}, ErrEndpointRequired
	}

	settings := grpcExporterSettings{
		Headers:     cloneMap(conf.Headers),
		Compression: conf.Compression,
		Timeout:     conf.Timeout,
		Insecure:    conf.Insecure,
	}

	if hasHTTPScheme(conf.Endpoint) {
		settings.EndpointURL = conf.Endpoint
		if parsed, err := url.Parse(conf.Endpoint); err == nil && parsed.Scheme == "http" {
			settings.Insecure = true
		}
	} else {
		settings.Endpoint = conf.Endpoint
	}

	return settings, nil
}

func defaultHTTPExporter(ctx context.Context, settings httpExporterSettings) (sdktrace.SpanExporter, error) {
	options := []otlptracehttp.Option{}
	if settings.EndpointURL != "" {
		options = append(options, otlptracehttp.WithEndpointURL(settings.EndpointURL))
	} else {
		options = append(options, otlptracehttp.WithEndpoint(settings.Endpoint))
	}
	if len(settings.Headers) > 0 {
		options = append(options, otlptracehttp.WithHeaders(settings.Headers))
	}
	if settings.Compression == CompressionGzip {
		options = append(options, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
	}
	if settings.Timeout > 0 {
		options = append(options, otlptracehttp.WithTimeout(settings.Timeout))
	}
	if settings.Insecure {
		options = append(options, otlptracehttp.WithInsecure())
	}
	return otlptracehttp.New(ctx, options...)
}

func defaultGRPCExporter(ctx context.Context, settings grpcExporterSettings) (sdktrace.SpanExporter, error) {
	options := []otlptracegrpc.Option{}
	if settings.EndpointURL != "" {
		options = append(options, otlptracegrpc.WithEndpointURL(settings.EndpointURL))
	} else {
		options = append(options, otlptracegrpc.WithEndpoint(settings.Endpoint))
	}
	if len(settings.Headers) > 0 {
		options = append(options, otlptracegrpc.WithHeaders(settings.Headers))
	}
	if settings.Compression == CompressionGzip {
		options = append(options, otlptracegrpc.WithCompressor("gzip"))
	}
	if settings.Timeout > 0 {
		options = append(options, otlptracegrpc.WithTimeout(settings.Timeout))
	}
	if settings.Insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, options...)
}

func buildSampler(sampleRate float64) sdktrace.Sampler {
	if sampleRate <= 0 {
		return sdktrace.NeverSample()
	}
	if sampleRate >= 1 {
		return sdktrace.AlwaysSample()
	}
	return sdktrace.TraceIDRatioBased(sampleRate)
}

func hasHTTPScheme(endpoint string) bool {
	value := strings.ToLower(strings.TrimSpace(endpoint))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func defaultServiceInstanceID() string {
	if hostname := strings.TrimSpace(os.Getenv("HOSTNAME")); hostname != "" {
		return hostname
	}

	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(hostname)
}
