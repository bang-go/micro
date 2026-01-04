package trace

import (
	"context"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type Config struct {
	ServiceName string
	Endpoint    string // e.g., "localhost:4317" or "https://host/path"
	Exporter    string // "otlp" or "stdout" (default: "stdout")
	SampleRate  float64
	Headers     map[string]string // Authorization headers
	Compression string            // "gzip"
	Timeout     time.Duration
}

// InitTracer initializes the global OpenTelemetry tracer provider.
// It returns a shutdown function that should be called when the application exits.
func InitTracer(ctx context.Context, conf *Config) (func(context.Context) error, error) {
	if conf == nil {
		conf = &Config{ServiceName: "unknown-service"}
	}
	if conf.Exporter == "" {
		conf.Exporter = "stdout"
	}

	var exporter sdktrace.SpanExporter
	var err error

	if conf.Exporter == "otlp" {
		// Detect protocol from Endpoint scheme
		endpoint := conf.Endpoint
		isHTTP := strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://")

		if isHTTP {
			// HTTP Exporter
			u, parseErr := url.Parse(endpoint)
			if parseErr != nil {
				return nil, parseErr
			}

			opts := []otlptracehttp.Option{
				otlptracehttp.WithEndpoint(u.Host),
				otlptracehttp.WithURLPath(u.Path),
			}

			if u.Scheme == "http" {
				opts = append(opts, otlptracehttp.WithInsecure())
			}

			if len(conf.Headers) > 0 {
				opts = append(opts, otlptracehttp.WithHeaders(conf.Headers))
			}

			if conf.Compression == "gzip" {
				opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
			}

			if conf.Timeout > 0 {
				opts = append(opts, otlptracehttp.WithTimeout(conf.Timeout))
			}

			exporter, err = otlptracehttp.New(ctx, opts...)
		} else {
			// gRPC Exporter (Default)
			opts := []otlptracegrpc.Option{
				otlptracegrpc.WithEndpoint(endpoint),
				otlptracegrpc.WithInsecure(), // Default to insecure for gRPC to match previous behavior
			}

			if len(conf.Headers) > 0 {
				opts = append(opts, otlptracegrpc.WithHeaders(conf.Headers))
			}

			if conf.Compression == "gzip" {
				opts = append(opts, otlptracegrpc.WithCompressor("gzip"))
			}

			if conf.Timeout > 0 {
				opts = append(opts, otlptracegrpc.WithTimeout(conf.Timeout))
			}

			exporter, err = otlptracegrpc.New(ctx, opts...)
		}
	} else {
		// Stdout exporter (useful for dev/debug)
		exporter, err = stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
	}

	if err != nil {
		return nil, err
	}

	// Resource (service info)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(conf.ServiceName),
			semconv.ServiceInstanceIDKey.String(os.Getenv("HOSTNAME")),
		),
	)
	if err != nil {
		return nil, err
	}

	// Sampler
	var sampler sdktrace.Sampler
	if conf.SampleRate > 0 {
		sampler = sdktrace.TraceIDRatioBased(conf.SampleRate)
	} else {
		sampler = sdktrace.AlwaysSample()
	}

	// Tracer Provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global provider
	otel.SetTracerProvider(tp)

	// Set global propagator (W3C Trace Context is standard)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
