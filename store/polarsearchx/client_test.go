package polarsearchx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewValidatesConfig(t *testing.T) {
	_, err := New(nil)
	if !errors.Is(err, ErrNilConfig) {
		t.Fatalf("expected ErrNilConfig, got %v", err)
	}

	_, err = New(&Config{Addresses: []string{" ", ""}})
	if !errors.Is(err, ErrAddressRequired) {
		t.Fatalf("expected ErrAddressRequired, got %v", err)
	}
}

func TestSearch(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	var capturedHeader string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedHeader = r.Header.Get("X-Trace")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"took":3,"timed_out":false,"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"plaza_product_search","_id":"42","_score":12.5,"_source":{"product_spu_id":42},"fields":{"x":[1]},"sort":[12.5,42]}]}}`))
	}))
	defer server.Close()

	header := http.Header{}
	header.Set("X-Trace", "trace-1")
	client, err := New(&Config{
		Addresses: []string{server.URL},
		Username:  "user",
		Password:  "pass",
		Header:    header,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Search(context.Background(), "plaza_product_search", map[string]any{
		"query": map[string]any{
			"match": map[string]any{"all_text": "苹果"},
		},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if capturedPath != "/plaza_product_search/_search" {
		t.Fatalf("path = %q", capturedPath)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if capturedAuth != wantAuth {
		t.Fatalf("authorization = %q, want %q", capturedAuth, wantAuth)
	}
	if capturedHeader != "trace-1" {
		t.Fatalf("X-Trace = %q", capturedHeader)
	}
	if capturedBody["query"] == nil {
		t.Fatalf("request body missing query: %#v", capturedBody)
	}
	if resp.Hits.Total.Value != 1 || len(resp.Hits.Hits) != 1 {
		t.Fatalf("unexpected hits: %#v", resp.Hits)
	}
	if string(resp.Hits.Hits[0].Source) != `{"product_spu_id":42}` {
		t.Fatalf("source = %s", resp.Hits.Hits[0].Source)
	}
}

func TestSearchResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad query"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := New(&Config{Addresses: []string{server.URL}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Search(context.Background(), "plaza_product_search", map[string]any{"query": "bad"})
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("expected ResponseError, got %v", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", responseErr.StatusCode)
	}
	if !strings.Contains(responseErr.Error(), "bad query") {
		t.Fatalf("error = %q", responseErr.Error())
	}
}

func TestRaw(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedContentType string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("X-Result", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"acknowledged":true}`))
	}))
	defer server.Close()

	client, err := New(&Config{Addresses: []string{server.URL}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Raw(context.Background(), http.MethodPut, "/plaza_product_search/_settings", map[string]any{"refresh_interval": "1s"})
	if err != nil {
		t.Fatalf("raw: %v", err)
	}

	if capturedMethod != http.MethodPut {
		t.Fatalf("method = %q", capturedMethod)
	}
	if capturedPath != "/plaza_product_search/_settings" {
		t.Fatalf("path = %q", capturedPath)
	}
	if capturedContentType != "application/json" {
		t.Fatalf("content-type = %q", capturedContentType)
	}
	if capturedBody["refresh_interval"] != "1s" {
		t.Fatalf("request body = %#v", capturedBody)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Result") != "ok" {
		t.Fatalf("response header = %#v", resp.Header)
	}
	if string(resp.Body) != `{"acknowledged":true}` {
		t.Fatalf("response body = %s", resp.Body)
	}
}

func TestRawResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"cluster blocked"}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := New(&Config{Addresses: []string{server.URL}})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Raw(context.Background(), http.MethodGet, "/_cluster/health", nil)
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("expected ResponseError, got %v", err)
	}
	if responseErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", responseErr.StatusCode)
	}
}
