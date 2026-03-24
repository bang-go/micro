package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestJSONLoggingIncludesTraceAndSource(t *testing.T) {
	var output bytes.Buffer
	log := New(
		WithOutput(&output),
		WithFormat("json"),
		WithLevel("debug"),
		WithAddSource(true),
	)

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: [16]byte{1, 2, 3},
		SpanID:  [8]byte{4, 5, 6},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
	log.Info(ctx, "hello", "component", "test")

	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, payload = %s", err, output.String())
	}
	if payload["msg"] != "hello" {
		t.Fatalf("expected log message, got %#v", payload["msg"])
	}
	if payload["trace_id"] == "" || payload["span_id"] == "" {
		t.Fatalf("expected trace context in payload, got %#v", payload)
	}
	source, ok := payload["source"].(map[string]any)
	if !ok {
		t.Fatalf("expected source metadata, got %#v", payload["source"])
	}
	file, _ := source["file"].(string)
	if file == "" || strings.Contains(file, "/") {
		t.Fatalf("expected basename source file, got %q", file)
	}
}

func TestToggleAndWithShareState(t *testing.T) {
	var output bytes.Buffer
	log := New(
		WithOutput(&output),
		WithFormat("text"),
		WithAddSource(false),
	)
	child := log.With("component", "worker")

	log.Toggle(false)
	child.Info(context.Background(), "hidden")
	if output.Len() != 0 {
		t.Fatalf("expected disabled logger to emit nothing, got %s", output.String())
	}

	log.Toggle(true)
	child.Info(context.Background(), "visible")
	if !strings.Contains(output.String(), "component=worker") || !strings.Contains(output.String(), "msg=visible") {
		t.Fatalf("unexpected text output: %s", output.String())
	}
}

func TestLevelFilteringAndBadArgs(t *testing.T) {
	var output bytes.Buffer
	log := New(
		WithOutput(&output),
		WithFormat("json"),
		WithLevel("warn"),
		WithAddSource(false),
	)

	log.Info(context.Background(), "skip")
	if output.Len() != 0 {
		t.Fatalf("expected info to be filtered, got %s", output.String())
	}

	log.Warn(context.Background(), "warn", "orphan")
	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["!BADKEY"] != "orphan" {
		t.Fatalf("expected !BADKEY field, got %#v", payload)
	}
}

func TestWithGroup(t *testing.T) {
	var output bytes.Buffer
	log := New(
		WithOutput(&output),
		WithFormat("json"),
		WithAddSource(false),
	)

	log.WithGroup("http").Info(context.Background(), "request", "method", "GET")

	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	group, ok := payload["http"].(map[string]any)
	if !ok || group["method"] != "GET" {
		t.Fatalf("expected grouped attrs, got %#v", payload)
	}
}

func TestGetSlogSharesToggleAndInjectsTraceContext(t *testing.T) {
	var output bytes.Buffer
	log := New(
		WithOutput(&output),
		WithFormat("json"),
		WithAddSource(false),
	)

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: [16]byte{7, 8, 9},
		SpanID:  [8]byte{1, 2, 3},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
	slogLogger := log.GetSlog()

	log.Toggle(false)
	slogLogger.InfoContext(ctx, "hidden")
	if output.Len() != 0 {
		t.Fatalf("expected disabled slog logger to emit nothing, got %s", output.String())
	}

	log.Toggle(true)
	slogLogger.InfoContext(ctx, "visible")

	var payload map[string]any
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, payload = %s", err, output.String())
	}
	if payload["msg"] != "visible" {
		t.Fatalf("expected log message, got %#v", payload["msg"])
	}
	if payload["trace_id"] == "" || payload["span_id"] == "" {
		t.Fatalf("expected trace context in payload, got %#v", payload)
	}
}
