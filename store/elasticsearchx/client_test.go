package elasticsearchx

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

func TestPrepareConfig(t *testing.T) {
	t.Run("validate config", func(t *testing.T) {
		_, err := prepareConfig(nil)
		if err != ErrNilConfig {
			t.Fatalf("expected ErrNilConfig, got %v", err)
		}

		_, err = prepareConfig(&Config{})
		if err != ErrAddressRequired {
			t.Fatalf("expected ErrAddressRequired, got %v", err)
		}
	})

	t.Run("clone and default headers", func(t *testing.T) {
		header := http.Header{
			"X-Test": []string{"value"},
		}
		cfg, err := prepareConfig(&Config{
			Addresses: []string{" http://localhost:9200 ", "http://localhost:9200", " "},
			Header:    header,
		})
		if err != nil {
			t.Fatalf("prepareConfig() error = %v", err)
		}
		if len(cfg.Addresses) != 1 || cfg.Addresses[0] != "http://localhost:9200" {
			t.Fatalf("unexpected addresses: %#v", cfg.Addresses)
		}
		if cfg.Header.Get("Accept") != "application/json" || cfg.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("expected default headers, got %#v", cfg.Header)
		}
		header.Set("X-Test", "mutated")
		if got, want := cfg.Header.Get("X-Test"), "value"; got != want {
			t.Fatalf("Header[X-Test] = %q, want %q", got, want)
		}
	})
}

func TestIndexLifecycleRequests(t *testing.T) {
	transport := &recordingTransport{
		handler: func(req *http.Request, body string) (*http.Response, error) {
			switch {
			case req.Method == http.MethodPut && req.URL.Path == "/products":
				if !strings.Contains(body, `"mappings"`) {
					t.Fatalf("expected create index body, got %s", body)
				}
				return jsonResponse(`{"acknowledged":true,"shards_acknowledged":true,"index":"products"}`), nil
			case req.Method == http.MethodHead && req.URL.Path == "/products":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"X-Elastic-Product": []string{"Elasticsearch"},
					},
					Body: io.NopCloser(strings.NewReader("")),
				}, nil
			case req.Method == http.MethodDelete && req.URL.Path == "/products":
				return jsonResponse(`{"acknowledged":true}`), nil
			default:
				t.Fatalf("unexpected request: %s %s body=%s", req.Method, req.URL.Path, body)
				return nil, nil
			}
		},
	}

	client, err := New(&Config{
		Addresses: []string{"http://example.com"},
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.CreateIndex(context.Background(), "products", map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"name": map[string]any{"type": "keyword"},
			},
		},
	}); err != nil {
		t.Fatalf("CreateIndex() error = %v", err)
	}

	exists, err := client.ExistsIndex(context.Background(), "products")
	if err != nil {
		t.Fatalf("ExistsIndex() error = %v", err)
	}
	if !exists {
		t.Fatal("expected index to exist")
	}

	if _, err := client.DeleteIndex(context.Background(), "products"); err != nil {
		t.Fatalf("DeleteIndex() error = %v", err)
	}
}

func TestDocumentSearchAndBulkRequests(t *testing.T) {
	transport := &recordingTransport{
		handler: func(req *http.Request, body string) (*http.Response, error) {
			switch {
			case req.Method == http.MethodPut && req.URL.Path == "/products/_doc/1":
				if !strings.Contains(body, `"name":"keyboard"`) {
					t.Fatalf("expected index body, got %s", body)
				}
				return jsonResponse(`{"_index":"products","_id":"1","result":"created","_version":1}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/products/_update/1":
				if !strings.Contains(body, `"doc":{"stock":10}`) {
					t.Fatalf("expected update body, got %s", body)
				}
				return jsonResponse(`{"_index":"products","_id":"1","result":"updated","_version":2}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/products/_search":
				if !strings.Contains(body, `"match_all"`) {
					t.Fatalf("expected search body, got %s", body)
				}
				return jsonResponse(`{"took":1,"timed_out":false,"hits":{"total":{"value":1,"relation":"eq"},"hits":[]}}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/_bulk":
				if !strings.Contains(body, `"index":{"_id":"2","_index":"products"}`) || !strings.Contains(body, `"delete":{"_id":"3","_index":"products"}`) {
					t.Fatalf("unexpected bulk body: %s", body)
				}
				return jsonResponse(`{"errors":false,"took":1,"items":[]}`), nil
			default:
				t.Fatalf("unexpected request: %s %s body=%s", req.Method, req.URL.Path, body)
				return nil, nil
			}
		},
	}

	client, err := New(&Config{
		Addresses: []string{"http://example.com"},
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.Index(context.Background(), "products", "1", map[string]any{"name": "keyboard"}); err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if _, err := client.Update(context.Background(), "products", "1", map[string]any{"stock": 10}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := client.Search(context.Background(), "products", &search.Request{
		Query: &types.Query{
			MatchAll: &types.MatchAllQuery{},
		},
	}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if _, err := client.Bulk(context.Background(), []BulkOperation{
		{Action: "index", Index: "products", ID: "2", Document: map[string]any{"name": "mouse"}},
		{Action: "delete", Index: "products", ID: "3"},
	}); err != nil {
		t.Fatalf("Bulk() error = %v", err)
	}
}

func TestValidation(t *testing.T) {
	client, err := New(&Config{Addresses: []string{"http://example.com"}, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{}`), nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.CreateIndex(nil, "products", nil); err != ErrContextRequired {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.CreateIndex(context.Background(), " ", nil); err != ErrIndexRequired {
		t.Fatalf("expected ErrIndexRequired, got %v", err)
	}
	if _, err := client.Index(nil, "products", "", map[string]any{"name": "keyboard"}); err != ErrContextRequired {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Index(context.Background(), "products", "", nil); err != ErrDocumentRequired {
		t.Fatalf("expected ErrDocumentRequired, got %v", err)
	}
	if _, err := client.Get(nil, "products", "1"); err != ErrContextRequired {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Get(context.Background(), "products", " "); err != ErrIDRequired {
		t.Fatalf("expected ErrIDRequired, got %v", err)
	}
	if _, err := client.Search(nil, "products", &search.Request{}); err != ErrContextRequired {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Search(context.Background(), "products", nil); err != ErrSearchRequestRequired {
		t.Fatalf("expected ErrSearchRequestRequired, got %v", err)
	}
	if _, err := client.Bulk(nil, []BulkOperation{{Action: "delete", Index: "products", ID: "1"}}); err != ErrContextRequired {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Bulk(context.Background(), nil); err != ErrBulkOperationsRequired {
		t.Fatalf("expected ErrBulkOperationsRequired, got %v", err)
	}
}

type recordingTransport struct {
	handler func(*http.Request, string) (*http.Response, error)
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		read, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = read
	}
	return t.handler(req, string(body))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":      []string{"application/json"},
			"X-Elastic-Product": []string{"Elasticsearch"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}
