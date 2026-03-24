package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/httpx"
	"github.com/prometheus/client_golang/prometheus"
)

func TestClientDoJSONAndSnapshot(t *testing.T) {
	t.Parallel()

	type serverPayload struct {
		Query      url.Values `json:"query"`
		Body       string     `json:"body"`
		AuthHeader string     `json:"auth_header"`
		Cookie     string     `json:"cookie"`
	}

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := r.Header.Values("X-Test"), []string{"one", "two"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("x-test = %v, want %v", got, want)
		}
		if got, want := r.Header.Get("Content-Type"), httpx.ContentTypeJSON; got != want {
			t.Fatalf("content-type = %q, want %q", got, want)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		payload := serverPayload{
			Query:      r.URL.Query(),
			Body:       string(body),
			AuthHeader: r.Header.Get("Authorization"),
			Cookie:     r.Header.Get("Cookie"),
		}

		responseBody, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode response: %v", err)
		}

		header := make(http.Header)
		header.Add("X-Multi", "one")
		header.Add("X-Multi", "two")
		header.Add("Set-Cookie", (&http.Cookie{Name: "token", Value: "a"}).String())
		header.Add("Set-Cookie", (&http.Cookie{Name: "token", Value: "b"}).String())

		return &http.Response{
			StatusCode: http.StatusCreated,
			Status:     "201 Created",
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(string(responseBody))),
		}, nil
	})

	req := &httpx.Request{
		Method: httpx.MethodPost,
		URL:    "http://example.com/v1/resource?existing=1",
		Query:  url.Values{"expand": {"profile"}, "tag": {"a", "b"}},
		Header: http.Header{
			"X-Test": {"one", "two"},
		},
		BasicAuth: &httpx.BasicAuth{
			Username: "demo",
			Password: "secret",
		},
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: "secret-cookie"})
	if err := req.SetJSONBody(map[string]string{"name": "alice"}); err != nil {
		t.Fatalf("set json body: %v", err)
	}

	client := httpx.NewClient(&httpx.ClientConfig{
		Timeout: 5 * time.Second,
		HTTPClient: &http.Client{
			Transport: transport,
		},
	})
	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("client do: %v", err)
	}

	if !resp.Success() {
		t.Fatalf("expected success response, got status %d", resp.StatusCode)
	}
	if got, want := resp.StatusCode, http.StatusCreated; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if got, want := resp.Header.Values("X-Multi"), []string{"one", "two"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("x-multi = %v, want %v", got, want)
	}
	if got, want := len(resp.Cookies), 2; got != want {
		t.Fatalf("cookies len = %d, want %d", got, want)
	}
	if resp.Request == nil {
		t.Fatal("request snapshot is nil")
	}
	if got, want := resp.Request.Header.Get("Authorization"), "REDACTED"; got != want {
		t.Fatalf("authorization snapshot = %q, want %q", got, want)
	}
	if got, want := resp.Request.Header.Get("Cookie"), "REDACTED"; got != want {
		t.Fatalf("cookie snapshot = %q, want %q", got, want)
	}

	var payload serverPayload
	if err := resp.DecodeJSON(&payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if got, want := payload.Query.Get("existing"), "1"; got != want {
		t.Fatalf("existing query = %q, want %q", got, want)
	}
	if got, want := payload.Query["tag"], []string{"a", "b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("tag query = %v, want %v", got, want)
	}
	if got, want := payload.AuthHeader, "Basic ZGVtbzpzZWNyZXQ="; got != want {
		t.Fatalf("auth header = %q, want %q", got, want)
	}
	if !strings.Contains(payload.Cookie, "session=secret-cookie") {
		t.Fatalf("cookie header = %q, want session cookie", payload.Cookie)
	}
}

func TestClientResponseUsesEffectiveRequestAndRedactsURLCredentials(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch calls.Add(1) {
		case 1:
			header := make(http.Header)
			header.Set("Location", "http://user:secret@example.com/final")
			return &http.Response{
				StatusCode: http.StatusFound,
				Status:     "302 Found",
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		case 2:
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected call count %d", calls.Load())
			return nil, nil
		}
	})

	client := httpx.NewClient(&httpx.ClientConfig{
		HTTPClient: &http.Client{
			Transport: transport,
		},
	})

	resp, err := client.Do(context.Background(), &httpx.Request{
		Method: httpx.MethodGet,
		URL:    "http://user:secret@example.com/redirect",
	})
	if err != nil {
		t.Fatalf("client do: %v", err)
	}

	if got, want := resp.Request.URL, "http://example.com/final"; got != want {
		t.Fatalf("response request url = %q, want %q", got, want)
	}
}

func TestClientClonesProvidedTransport(t *testing.T) {
	t.Parallel()

	transport := &http.Transport{MaxIdleConns: 3}
	client := httpx.NewClient(&httpx.ClientConfig{
		Transport:       transport,
		MaxIdleConns:    99,
		IdleConnTimeout: 2 * time.Minute,
	})

	httpClient := client.HTTPClient()
	cloned, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", httpClient.Transport)
	}
	if cloned == transport {
		t.Fatal("expected cloned transport, got original pointer")
	}
	if got, want := transport.MaxIdleConns, 3; got != want {
		t.Fatalf("original transport max idle conns = %d, want %d", got, want)
	}
	if got, want := cloned.MaxIdleConns, 99; got != want {
		t.Fatalf("cloned transport max idle conns = %d, want %d", got, want)
	}
}

func TestClientCloseIdleConnectionsPreservedThroughTracing(t *testing.T) {
	t.Parallel()

	transport := &closingRoundTripper{}
	client := httpx.NewClient(&httpx.ClientConfig{
		Trace: true,
		HTTPClient: &http.Client{
			Transport: transport,
		},
	})

	resp, err := client.Do(context.Background(), &httpx.Request{
		Method: httpx.MethodGet,
		URL:    "http://example.com/healthz",
	})
	if err != nil {
		t.Fatalf("client do: %v", err)
	}
	if got, want := resp.StatusCode, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	client.CloseIdleConnections()
	if !transport.closed.Load() {
		t.Fatal("expected CloseIdleConnections to be forwarded to underlying transport")
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	var req *httpx.Request
	if _, err := req.Build(context.Background()); !errors.Is(err, httpx.ErrNilRequest) {
		t.Fatalf("nil request error = %v, want %v", err, httpx.ErrNilRequest)
	}

	_, err := (&httpx.Request{URL: "http://example.com"}).Build(context.Background())
	if !errors.Is(err, httpx.ErrRequestMethodRequired) {
		t.Fatalf("missing method error = %v, want %v", err, httpx.ErrRequestMethodRequired)
	}

	_, err = (&httpx.Request{Method: httpx.MethodGet}).Build(context.Background())
	if !errors.Is(err, httpx.ErrRequestURLRequired) {
		t.Fatalf("missing url error = %v, want %v", err, httpx.ErrRequestURLRequired)
	}

	_, err = (&httpx.Request{Method: httpx.MethodGet, URL: "/relative"}).Build(context.Background())
	if !errors.Is(err, httpx.ErrRequestAbsoluteURLRequired) {
		t.Fatalf("relative url error = %v, want %v", err, httpx.ErrRequestAbsoluteURLRequired)
	}

	_, err = (&httpx.Request{Method: httpx.MethodGet, URL: "http://example.com"}).Build(nil)
	if !errors.Is(err, httpx.ErrContextRequired) {
		t.Fatalf("nil context error = %v, want %v", err, httpx.ErrContextRequired)
	}
}

func TestClientDoRequiresContext(t *testing.T) {
	t.Parallel()

	client := httpx.NewClient(&httpx.ClientConfig{
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				t.Fatal("round tripper should not be called for nil context")
				return nil, nil
			}),
		},
	})

	_, err := client.Do(nil, &httpx.Request{
		Method: httpx.MethodGet,
		URL:    "http://example.com",
	})
	if !errors.Is(err, httpx.ErrContextRequired) {
		t.Fatalf("Do(nil) error = %v, want %v", err, httpx.ErrContextRequired)
	}
}

func TestClientMetricsRegistererAndDisableMetrics(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	_ = httpx.NewClient(&httpx.ClientConfig{
		MetricsRegisterer: reg,
	})

	assertCollectorRegistered(t, reg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "httpx_client_request_duration_seconds",
			Help: "HTTP client request duration in seconds.",
		},
		[]string{"method", "code"},
	))
	assertCollectorRegistered(t, reg, prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "httpx_client_requests_total",
			Help: "Total number of HTTP client requests.",
		},
		[]string{"method", "code"},
	))

	disabledReg := prometheus.NewRegistry()
	_ = httpx.NewClient(&httpx.ClientConfig{
		DisableMetrics:    true,
		MetricsRegisterer: disabledReg,
	})

	assertCollectorNotRegistered(t, disabledReg, prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "httpx_client_request_duration_seconds",
			Help: "HTTP client request duration in seconds.",
		},
		[]string{"method", "code"},
	))
}

type closingRoundTripper struct {
	closed atomic.Bool
}

func (c *closingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Status:     http.StatusText(http.StatusNoContent),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func (c *closingRoundTripper) CloseIdleConnections() {
	c.closed.Store(true)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
