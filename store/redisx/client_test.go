package redisx

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOpenValidation(t *testing.T) {
	_, err := Open(nil, &Config{Addr: "127.0.0.1:6379"})
	if !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Open(nil, ...) error = %v, want %v", err, ErrContextRequired)
	}

	_, err = Open(context.Background(), nil)
	if !errors.Is(err, ErrNilConfig) {
		t.Fatalf("Open(nil) error = %v, want %v", err, ErrNilConfig)
	}

	_, err = New(&Config{})
	if !errors.Is(err, ErrAddrRequired) {
		t.Fatalf("New missing addr error = %v, want %v", err, ErrAddrRequired)
	}
}

func TestPrepareConfigNormalizesAndClonesInput(t *testing.T) {
	conf := &Config{
		Name:            " cache ",
		Addr:            " 127.0.0.1:6379 ",
		Username:        " user ",
		ClientName:      " redis-client ",
		IdentitySuffix:  " suffix ",
		TraceAttributes: []attribute.KeyValue{attribute.String("env", "prod")},
	}

	normalized, opts, err := prepareConfig(conf)
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}

	if got, want := normalized.Name, "cache"; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := opts.Addr, "127.0.0.1:6379"; got != want {
		t.Fatalf("opts.Addr = %q, want %q", got, want)
	}
	if got, want := opts.Username, "user"; got != want {
		t.Fatalf("opts.Username = %q, want %q", got, want)
	}
	if got, want := opts.ClientName, "redis-client"; got != want {
		t.Fatalf("opts.ClientName = %q, want %q", got, want)
	}
	if got, want := opts.IdentitySuffix, "suffix"; got != want {
		t.Fatalf("opts.IdentitySuffix = %q, want %q", got, want)
	}
	if got, want := len(normalized.TraceAttributes), 1; got != want {
		t.Fatalf("len(TraceAttributes) = %d, want %d", got, want)
	}

	conf.Name = "mutated"
	conf.TraceAttributes[0] = attribute.String("env", "dev")
	if got, want := normalized.Name, "cache"; got != want {
		t.Fatalf("normalized.Name = %q, want %q", got, want)
	}
	if got, want := normalized.TraceAttributes[0].Value.AsString(), "prod"; got != want {
		t.Fatalf("normalized.TraceAttributes[0] = %q, want %q", got, want)
	}
}

func TestPrepareConfigUsesUnixNetworkForSocketPath(t *testing.T) {
	conf, opts, err := prepareConfig(&Config{Addr: "/tmp/redis.sock"})
	if err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}
	if conf == nil {
		t.Fatal("prepareConfig returned nil config")
	}
	if got, want := opts.Network, "unix"; got != want {
		t.Fatalf("opts.Network = %q, want %q", got, want)
	}
}

func TestPrepareConfigAllowsExplicitIdentityEnable(t *testing.T) {
	conf, opts, err := prepareConfig(&Config{
		Addr:            "127.0.0.1:6379",
		DisableIdentity: util.Ptr(false),
	})
	if err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}
	if conf == nil {
		t.Fatal("prepareConfig returned nil config")
	}
	if opts.DisableIdentity {
		t.Fatal("opts.DisableIdentity = true, want explicit false")
	}
}

func TestClientLifecycleWithFakeServer(t *testing.T) {
	server := newFakeRedisServer()

	client, err := Open(context.Background(), &Config{
		Name:            "cache",
		Addr:            "pipe",
		Protocol:        2,
		DisableIdentity: util.Ptr(true),
		Dialer:          server.dialer,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer client.Close()

	rdb := client.Redis()
	if err := rdb.Set(context.Background(), "hello", "world", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	value, err := rdb.Get(context.Background(), "hello").Result()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got, want := value, "world"; got != want {
		t.Fatalf("value = %q, want %q", got, want)
	}
	if err := rdb.Del(context.Background(), "hello").Err(); err != nil {
		t.Fatalf("del: %v", err)
	}

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if client.Redis() == nil {
		t.Fatal("Redis() returned nil")
	}
	if opts := client.Options(); opts == nil || opts.Addr != "pipe" {
		t.Fatalf("Options() = %+v, want addr=pipe", opts)
	}
	if stats := client.Stats(); stats.Misses < 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close idempotent: %v", err)
	}

	commands := server.commandLog()
	if got, want := businessCommandNames(commands), []string{"PING", "SET", "GET", "DEL", "PING"}; !slices.Equal(got, want) {
		t.Fatalf("business commands = %v, want %v (raw=%v)", got, want, commands)
	}
}

func TestAddHookValidationAndInvocation(t *testing.T) {
	server := newFakeRedisServer()
	client, err := New(&Config{
		Addr:            "pipe",
		Protocol:        2,
		DisableIdentity: util.Ptr(true),
		Dialer:          server.dialer,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()

	if err := client.AddHook(nil); !errors.Is(err, ErrNilHook) {
		t.Fatalf("AddHook(nil) error = %v, want %v", err, ErrNilHook)
	}

	hook := &spyHook{}
	if err := client.AddHook(hook); err != nil {
		t.Fatalf("AddHook(spy): %v", err)
	}
	if err := client.Redis().Set(context.Background(), "k", "v", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if hook.processCalls == 0 {
		t.Fatal("spy hook was not invoked")
	}
}

func TestPingRejectsNilContext(t *testing.T) {
	server := newFakeRedisServer()
	client, err := New(&Config{
		Addr:            "pipe",
		Protocol:        2,
		DisableIdentity: util.Ptr(true),
		Dialer:          server.dialer,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()

	if err := client.Ping(nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Ping(nil) error = %v, want %v", err, ErrContextRequired)
	}
}

func TestObservabilityHookRedactsValues(t *testing.T) {
	var logs safeBuffer
	h := &observabilityHook{
		name:          "cache",
		addr:          "127.0.0.1:6379",
		logger:        logger.New(logger.WithFormat("text"), logger.WithAddSource(false), logger.WithLevel("debug"), logger.WithOutput(&logs)),
		enableLogger:  true,
		slowThreshold: time.Second,
	}

	cmd := redis.NewStatusCmd(context.Background(), "SET", "token", "very-secret")
	err := h.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
		return nil
	})(context.Background(), cmd)
	if err != nil {
		t.Fatalf("ProcessHook: %v", err)
	}

	output := logs.String()
	if strings.Contains(output, "very-secret") || strings.Contains(output, "token") {
		t.Fatalf("log leaked command values: %q", output)
	}
	if !strings.Contains(output, "command=set") {
		t.Fatalf("log missing command: %q", output)
	}
}

func TestObservabilityHookSkipsConnectionManagementCommands(t *testing.T) {
	var logs safeBuffer
	h := &observabilityHook{
		name:          "cache",
		addr:          "127.0.0.1:6379",
		logger:        logger.New(logger.WithFormat("text"), logger.WithAddSource(false), logger.WithLevel("debug"), logger.WithOutput(&logs)),
		enableLogger:  true,
		slowThreshold: time.Second,
	}

	expected := errors.New("ERR unknown command")
	cmd := redis.NewMapStringInterfaceCmd(context.Background(), "HELLO", 2)
	err := h.ProcessHook(func(ctx context.Context, cmd redis.Cmder) error {
		return expected
	})(context.Background(), cmd)
	if !errors.Is(err, expected) {
		t.Fatalf("ProcessHook error = %v, want %v", err, expected)
	}
	if output := logs.String(); output != "" {
		t.Fatalf("connection-management command should not be logged: %q", output)
	}
}

func TestPipelineStatusAndLogs(t *testing.T) {
	var logs safeBuffer
	h := &observabilityHook{
		name:          "cache",
		addr:          "127.0.0.1:6379",
		logger:        logger.New(logger.WithFormat("text"), logger.WithAddSource(false), logger.WithLevel("debug"), logger.WithOutput(&logs)),
		enableLogger:  true,
		slowThreshold: time.Second,
	}

	getCmd := redis.NewStringCmd(context.Background(), "GET", "missing")
	getCmd.SetErr(redis.Nil)
	setCmd := redis.NewStatusCmd(context.Background(), "SET", "token", "secret")

	err := h.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
		return nil
	})(context.Background(), []redis.Cmder{getCmd, setCmd})
	if err != nil {
		t.Fatalf("ProcessPipelineHook: %v", err)
	}

	output := logs.String()
	if strings.Contains(output, "secret") || strings.Contains(output, "missing") {
		t.Fatalf("pipeline log leaked values: %q", output)
	}
	if !strings.Contains(output, "commands=\"[get set]\"") && !strings.Contains(output, "commands=[get set]") {
		t.Fatalf("pipeline log missing command names: %q", output)
	}
}

func TestPipelineLogsObservedCommandCount(t *testing.T) {
	var logs safeBuffer
	h := &observabilityHook{
		name:          "cache",
		addr:          "127.0.0.1:6379",
		logger:        logger.New(logger.WithFormat("text"), logger.WithAddSource(false), logger.WithLevel("debug"), logger.WithOutput(&logs)),
		enableLogger:  true,
		slowThreshold: time.Second,
	}

	firstSet := redis.NewStatusCmd(context.Background(), "SET", "k1", "v1")
	secondSet := redis.NewStatusCmd(context.Background(), "SET", "k2", "v2")

	err := h.ProcessPipelineHook(func(ctx context.Context, cmds []redis.Cmder) error {
		return nil
	})(context.Background(), []redis.Cmder{firstSet, secondSet})
	if err != nil {
		t.Fatalf("ProcessPipelineHook: %v", err)
	}

	output := logs.String()
	if !strings.Contains(output, "command_count=2") {
		t.Fatalf("pipeline log missing observed command count: %q", output)
	}
}

func TestTraceDoesNotIncludeCommandArgsByDefault(t *testing.T) {
	server := newFakeRedisServer()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider()
	provider.RegisterSpanProcessor(recorder)
	defer provider.Shutdown(context.Background())

	client, err := New(&Config{
		Name:            "cache",
		Addr:            "pipe",
		Protocol:        2,
		DisableIdentity: util.Ptr(true),
		Dialer:          server.dialer,
		SkipPing:        true,
		Trace:           true,
		TraceProvider:   provider,
		TraceAttributes: []attribute.KeyValue{attribute.String("component", "redisx-test")},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()

	if err := client.Redis().Set(context.Background(), "secret-key", "secret-value", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected spans")
	}
	foundSet := false
	for _, span := range spans {
		if strings.EqualFold(span.Name(), "set") {
			foundSet = true
		}
		if strings.EqualFold(span.Name(), "hello") || strings.HasPrefix(span.Name(), "redis.dial") {
			t.Fatalf("trace should filter connection-management spans: %q", span.Name())
		}
		for _, attr := range span.Attributes() {
			if strings.Contains(attr.Value.Emit(), "secret-value") || strings.Contains(attr.Value.Emit(), "secret-key") {
				t.Fatalf("trace leaked command args: %+v", span.Attributes())
			}
		}
	}
	if !foundSet {
		t.Fatalf("expected set span, spans=%v", spanNames(spans))
	}
}

func TestMetricsRegistererAndDisableMetrics(t *testing.T) {
	server := newFakeRedisServer()
	reg := prometheus.NewRegistry()
	client, err := New(&Config{
		Addr:              "pipe",
		Protocol:          2,
		DisableIdentity:   util.Ptr(true),
		Dialer:            server.dialer,
		MetricsRegisterer: reg,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer client.Close()
	if err := client.Redis().Set(context.Background(), "k", "v", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	var foundDuration bool
	var foundTotal bool
	for _, family := range families {
		switch family.GetName() {
		case "redisx_request_duration_seconds":
			foundDuration = true
		case "redisx_requests_total":
			foundTotal = true
		}
	}
	if !foundDuration || !foundTotal {
		t.Fatalf("redisx metrics missing: duration=%v total=%v", foundDuration, foundTotal)
	}

	disabledReg := prometheus.NewRegistry()
	disabledClient, err := New(&Config{
		Addr:              "pipe",
		Protocol:          2,
		DisableIdentity:   util.Ptr(true),
		Dialer:            server.dialer,
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})
	if err != nil {
		t.Fatalf("new(disable metrics): %v", err)
	}
	defer disabledClient.Close()
	if err := disabledClient.Redis().Set(context.Background(), "k2", "v2", 0).Err(); err != nil {
		t.Fatalf("set(disable metrics): %v", err)
	}

	families, err = disabledReg.Gather()
	if err != nil {
		t.Fatalf("gather(disable metrics): %v", err)
	}
	for _, family := range families {
		if family.GetName() == "redisx_request_duration_seconds" || family.GetName() == "redisx_requests_total" {
			t.Fatalf("disabled metrics should not be registered, got %s", family.GetName())
		}
	}
}

func TestNewCanBeCalledRepeatedly(t *testing.T) {
	server := newFakeRedisServer()
	first, err := New(&Config{Addr: "pipe", Protocol: 2, Dialer: server.dialer})
	if err != nil {
		t.Fatalf("first new: %v", err)
	}
	defer first.Close()

	second, err := New(&Config{
		Options: &redis.Options{
			Addr:            "pipe",
			Protocol:        2,
			DisableIdentity: true,
			Dialer:          server.dialer,
		},
	})
	if err != nil {
		t.Fatalf("second new: %v", err)
	}
	defer second.Close()
}

type spyHook struct {
	processCalls int
}

func businessCommandNames(commands [][]string) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		name := strings.ToUpper(command[0])
		switch name {
		case "AUTH", "HELLO":
			continue
		case "CLIENT":
			if len(command) > 1 {
				subcommand := strings.ToUpper(command[1])
				if subcommand == "SETINFO" || subcommand == "SETNAME" {
					continue
				}
			}
		}
		names = append(names, name)
	}
	return names
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}

func (h *spyHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *spyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.processCalls++
		return next(ctx, cmd)
	}
}

func (h *spyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
