package grpcx_test

import (
	"context"
	"io"
	"testing"
	"time"

	clientinterceptor "github.com/bang-go/micro/transport/grpcx/client_interceptor"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type fakeClientStream struct {
	ctx        context.Context
	recvMsgErr error
}

func (f *fakeClientStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (f *fakeClientStream) Trailer() metadata.MD         { return metadata.MD{} }
func (f *fakeClientStream) CloseSend() error             { return nil }
func (f *fakeClientStream) Context() context.Context     { return f.ctx }
func (f *fakeClientStream) SendMsg(any) error            { return nil }
func (f *fakeClientStream) RecvMsg(any) error            { return f.recvMsgErr }

func TestStreamClientMetricInterceptorRecordsOnEOF(t *testing.T) {
	metrics := clientinterceptor.NewMetrics(nil)
	interceptor := clientinterceptor.StreamClientMetricInterceptorWithMetrics(metrics)

	const (
		method = "/svc.Stream/Recv"
		code   = "OK"
	)

	before := counterValue(t, metrics.RequestsTotal.WithLabelValues(method, code))
	stream, err := interceptor(context.Background(), &grpc.StreamDesc{ServerStreams: true}, nil, method, func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		return &fakeClientStream{
			ctx:        context.Background(),
			recvMsgErr: io.EOF,
		}, nil
	})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	if err := stream.RecvMsg(nil); err != io.EOF {
		t.Fatalf("RecvMsg() error = %v, want %v", err, io.EOF)
	}

	after := counterValue(t, metrics.RequestsTotal.WithLabelValues(method, code))
	if after-before != 1 {
		t.Fatalf("ClientRequestsTotal delta = %v, want 1", after-before)
	}
}

func TestStreamClientMetricInterceptorRecordsCancellationFromStreamContext(t *testing.T) {
	metrics := clientinterceptor.NewMetrics(nil)
	interceptor := clientinterceptor.StreamClientMetricInterceptorWithMetrics(metrics)

	const (
		method = "/svc.Stream/Cancel"
		code   = "Canceled"
	)

	ctx, cancel := context.WithCancel(context.Background())
	before := counterValue(t, metrics.RequestsTotal.WithLabelValues(method, code))

	stream, err := interceptor(ctx, &grpc.StreamDesc{ServerStreams: true}, nil, method, func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		return &fakeClientStream{ctx: ctx}, nil
	})
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if stream == nil {
		t.Fatal("expected wrapped client stream")
	}

	cancel()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		after := counterValue(t, metrics.RequestsTotal.WithLabelValues(method, code))
		if after-before == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	after := counterValue(t, metrics.RequestsTotal.WithLabelValues(method, code))
	t.Fatalf("ClientRequestsTotal delta = %v, want 1", after-before)
}

func counterValue(t *testing.T, collector interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()

	metric := &dto.Metric{}
	if err := collector.Write(metric); err != nil {
		t.Fatalf("collector.Write() error = %v", err)
	}
	if metric.Counter == nil {
		t.Fatal("expected counter metric")
	}

	return metric.Counter.GetValue()
}
