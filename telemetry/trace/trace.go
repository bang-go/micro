package trace

import (
	"context"
	"os"

	"github.com/bang-go/micro/telemetry/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type Config struct {
	ServiceName string
	Endpoint    string // e.g., "localhost:4317"
	Exporter    string // "otlp" or "stdout" (default: "stdout")
	SampleRate  float64
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
		// OTLP gRPC exporter
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(conf.Endpoint),
			otlptracegrpc.WithInsecure(), // In production, consider TLS
		)
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

	logger.New().Info(context.Background(), "trace_initialized", "service", conf.ServiceName, "exporter", conf.Exporter)

	return tp.Shutdown, nil
}
