package trace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bang-go/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestPrepareConfig(t *testing.T) {
	cfg, err := prepareConfig(nil)
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if cfg.ServiceName != "unknown-service" || cfg.Exporter != ExporterStdout {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.SampleRate == nil || *cfg.SampleRate != 1 {
		t.Fatalf("default sample rate = %v, want 1", cfg.SampleRate)
	}

	_, err = prepareConfig(&Config{SampleRate: util.Ptr(2.0)})
	if !errors.Is(err, ErrInvalidSampleRate) {
		t.Fatalf("expected ErrInvalidSampleRate, got %v", err)
	}
}

func TestExporterKindAndSettings(t *testing.T) {
	kind, err := exporterKind(&Config{Exporter: ExporterOTLP, Endpoint: "https://otel.example.com/v1/traces"})
	if err != nil {
		t.Fatalf("exporterKind() error = %v", err)
	}
	if kind != ExporterOTLPHTTP {
		t.Fatalf("expected ExporterOTLPHTTP, got %s", kind)
	}

	httpSettings, err := buildHTTPExporterSettings(&Config{
		Endpoint:    "http://otel.example.com/v1/traces",
		Headers:     map[string]string{"Authorization": "token"},
		Compression: CompressionGzip,
		Timeout:     3 * time.Second,
	})
	if err != nil {
		t.Fatalf("buildHTTPExporterSettings() error = %v", err)
	}
	if httpSettings.EndpointURL != "http://otel.example.com/v1/traces" || !httpSettings.Insecure || httpSettings.Headers["Authorization"] != "token" {
		t.Fatalf("unexpected http settings: %+v", httpSettings)
	}

	grpcSettings, err := buildGRPCExporterSettings(&Config{
		Endpoint:    "collector:4317",
		Insecure:    true,
		Compression: CompressionGzip,
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatalf("buildGRPCExporterSettings() error = %v", err)
	}
	if grpcSettings.Endpoint != "collector:4317" || !grpcSettings.Insecure || grpcSettings.Compression != CompressionGzip {
		t.Fatalf("unexpected grpc settings: %+v", grpcSettings)
	}
}

func TestOpenWithInjectedHTTPExporter(t *testing.T) {
	fake := &fakeExporter{}
	var captured httpExporterSettings

	globalProvider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(globalProvider)
	globalPropagator := nopPropagator{}
	otel.SetTextMapPropagator(globalPropagator)

	tp, err := Open(context.Background(), &Config{
		ServiceName: "svc",
		Exporter:    ExporterOTLPHTTP,
		Endpoint:    "https://otel.example.com/v1/traces",
		Headers:     map[string]string{"Authorization": "token"},
		Compression: CompressionGzip,
		Timeout:     5 * time.Second,
		newHTTPExporter: func(_ context.Context, settings httpExporterSettings) (sdktrace.SpanExporter, error) {
			captured = settings
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if captured.EndpointURL != "https://otel.example.com/v1/traces" || captured.Headers["Authorization"] != "token" {
		t.Fatalf("unexpected captured settings: %+v", captured)
	}
	if got := otel.GetTracerProvider(); got != globalProvider {
		t.Fatal("Open() should not mutate global tracer provider")
	}
	if _, ok := otel.GetTextMapPropagator().(nopPropagator); !ok {
		t.Fatal("Open() should not mutate global propagator")
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if !fake.shutdown {
		t.Fatal("expected exporter shutdown to be called")
	}
}

func TestInitTracerUsesStdoutFactory(t *testing.T) {
	fake := &fakeExporter{}
	globalProvider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(globalProvider)
	otel.SetTextMapPropagator(nopPropagator{})

	shutdown, err := InitTracer(context.Background(), &Config{
		ServiceName: "svc",
		Exporter:    ExporterStdout,
		newStdoutExporter: func(*Config) (sdktrace.SpanExporter, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("InitTracer() error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function")
	}
	if got := otel.GetTracerProvider(); got == globalProvider {
		t.Fatal("InitTracer() should replace global tracer provider")
	}
	if _, ok := otel.GetTextMapPropagator().(nopPropagator); ok {
		t.Fatal("InitTracer() should replace global propagator")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}
	if !fake.shutdown {
		t.Fatal("expected fake exporter to be shutdown")
	}
}

func TestOpenRejectsNilContext(t *testing.T) {
	if _, err := Open(nil, nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Open(nil) error = %v, want %v", err, ErrContextRequired)
	}
	if _, err := InitTracer(nil, nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("InitTracer(nil) error = %v, want %v", err, ErrContextRequired)
	}
}

func TestBuildSampler(t *testing.T) {
	if got, want := buildSampler(0).Description(), sdktrace.NeverSample().Description(); got != want {
		t.Fatalf("buildSampler(0) = %q, want %q", got, want)
	}
	if got, want := buildSampler(1).Description(), sdktrace.AlwaysSample().Description(); got != want {
		t.Fatalf("buildSampler(1) = %q, want %q", got, want)
	}
	if got, want := buildSampler(0.5).Description(), sdktrace.TraceIDRatioBased(0.5).Description(); got != want {
		t.Fatalf("buildSampler(0.5) = %q, want %q", got, want)
	}
}

type fakeExporter struct {
	shutdown bool
}

func (f *fakeExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return nil
}

func (f *fakeExporter) Shutdown(context.Context) error {
	f.shutdown = true
	return nil
}

type nopPropagator struct{}

func (nopPropagator) Inject(context.Context, propagation.TextMapCarrier) {}

func (nopPropagator) Extract(ctx context.Context, _ propagation.TextMapCarrier) context.Context {
	return ctx
}

func (nopPropagator) Fields() []string { return nil }
