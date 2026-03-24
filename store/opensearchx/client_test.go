package opensearchx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPrepareConfig(t *testing.T) {
	t.Run("validate config", func(t *testing.T) {
		_, err := prepareConfig(nil)
		if !errors.Is(err, ErrNilConfig) {
			t.Fatalf("expected ErrNilConfig, got %v", err)
		}

		_, err = prepareConfig(&Config{})
		if !errors.Is(err, ErrEndpointRequired) {
			t.Fatalf("expected ErrEndpointRequired, got %v", err)
		}
	})

	t.Run("default clean values", func(t *testing.T) {
		cfg, err := prepareConfig(&Config{
			Endpoint:        " opensearch-cn-hangzhou.aliyuncs.com ",
			AccessKeyID:     " ak ",
			AccessKeySecret: " sk ",
		})
		if err != nil {
			t.Fatalf("prepareConfig() error = %v", err)
		}
		if cfg.Endpoint != "opensearch-cn-hangzhou.aliyuncs.com" || cfg.Protocol != defaultProtocol {
			t.Fatalf("unexpected config defaults: %+v", cfg)
		}
		if cfg.ConnectTimeout != defaultConnectTimeout || cfg.ReadTimeout != defaultReadTimeout || cfg.MaxIdleConns != defaultMaxIdleConns {
			t.Fatalf("unexpected timeout defaults: %+v", cfg)
		}
	})

	t.Run("reject invalid protocol", func(t *testing.T) {
		_, err := prepareConfig(&Config{
			Endpoint:        "opensearch-cn-hangzhou.aliyuncs.com",
			Protocol:        "ftp",
			AccessKeyID:     "ak",
			AccessKeySecret: "sk",
		})
		if !errors.Is(err, ErrInvalidProtocol) {
			t.Fatalf("expected ErrInvalidProtocol, got %v", err)
		}
	})
}

func TestQueryBuilders(t *testing.T) {
	threshold := 0.82
	query := (&SearchRequest{
		Query: &QueryClause{Index: "default", Value: "iphone"},
		Filter: &FilterClause{
			Field:    "status",
			Operator: "=",
			Value:    1,
		},
		Sort: &SortClause{
			Field: "price",
			Order: "-",
		},
		Summary: &SummaryConfig{
			SummaryField:   "title",
			SummaryElement: "em",
		},
		Hit:             20,
		Config:          "rerank_size:200",
		FetchFields:     "title;price",
		VectorThreshold: &threshold,
	}).String()

	if query == "" {
		t.Fatal("expected query string to be built")
	}
	for _, fragment := range []string{
		"query=default:'iphone'",
		"config=start:0,hit:20,format:fulljson,rerank_size:200",
		"filter=status=1",
		"sort=price:-",
		"fetch_fields=title;price",
		"summary=summary_field:title,summary_element:em",
		"vector_threshold=0.82",
	} {
		if !contains(query, fragment) {
			t.Fatalf("expected query string to contain %q, got %s", fragment, query)
		}
	}
	if strings.Count(query, "config=") != 1 {
		t.Fatalf("expected a single config clause, got %s", query)
	}
}

func TestClientRequestBuilding(t *testing.T) {
	fake := &fakeRequester{
		handler: func(req requestSpec) map[string]any {
			switch req.Path {
			case "/v3/openapi/apps/catalog/search":
				return map[string]any{
					"body": map[string]any{
						"request_id": "req-1",
						"status":     "OK",
						"result": map[string]any{
							"items": []map[string]any{{"name": "book"}},
						},
					},
				}
			case "/v3/openapi/apps/catalog/suggest/suggest/search":
				return map[string]any{
					"body": map[string]any{
						"suggestions": []map[string]any{{"suggestion": "book"}},
					},
				}
			case "/v3/openapi/apps/catalog/actions/hint":
				return map[string]any{
					"body": map[string]any{
						"result": []map[string]any{{"hint": "book"}},
					},
				}
			case "/v3/openapi/apps/catalog/actions/hot":
				return map[string]any{
					"body": map[string]any{
						"result": []map[string]any{{"hot": "book"}},
					},
				}
			default:
				return map[string]any{}
			}
		},
	}
	client, err := New(&Config{
		Endpoint:        "endpoint",
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		newRequester: func(*Config) requester {
			return fake
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.Search(context.Background(), "catalog", &SearchRequest{
		Query: &QueryClause{Index: "default", Value: "1"},
	}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if fake.last.Path != "/v3/openapi/apps/catalog/search" || fake.last.Query["query"] == "" {
		t.Fatalf("unexpected search request: %+v", fake.last)
	}

	if _, err := client.Suggest(context.Background(), "catalog", "suggest", &SuggestRequest{Query: "1", Hit: 5}); err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if fake.last.Path != "/v3/openapi/apps/catalog/suggest/suggest/search" || fake.last.Query["hit"] != "5" {
		t.Fatalf("unexpected suggest request: %+v", fake.last)
	}

	if _, err := client.Hint(context.Background(), "catalog", &HintRequest{Hit: 3, SortType: "pv"}); err != nil {
		t.Fatalf("Hint() error = %v", err)
	}
	if fake.last.Path != "/v3/openapi/apps/catalog/actions/hint" || fake.last.Query["sort_type"] != "pv" {
		t.Fatalf("unexpected hint request: %+v", fake.last)
	}

	if _, err := client.HotSearch(context.Background(), "catalog", &HotSearchRequest{Hit: 6}); err != nil {
		t.Fatalf("HotSearch() error = %v", err)
	}
	if fake.last.Path != "/v3/openapi/apps/catalog/actions/hot" || fake.last.Query["hit"] != "6" {
		t.Fatalf("unexpected hot request: %+v", fake.last)
	}

	typed, err := SearchTyped[struct {
		Name string `json:"name"`
	}](client, context.Background(), "catalog", &SearchRequest{
		Query: &QueryClause{Index: "default", Value: "1"},
	})
	if err != nil {
		t.Fatalf("SearchTyped() error = %v", err)
	}
	if len(typed.Body.Result.Items) != 1 || typed.Body.Result.Items[0].Name != "book" {
		t.Fatalf("unexpected typed search response: %+v", typed.Body.Result.Items)
	}
}

func TestValidation(t *testing.T) {
	client, err := New(&Config{
		Endpoint:        "endpoint",
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		newRequester: func(*Config) requester {
			return &fakeRequester{}
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.Search(nil, "", &SearchRequest{Query: &QueryClause{Index: "default", Value: "x"}}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Search(context.Background(), "", &SearchRequest{Query: &QueryClause{Index: "default", Value: "x"}}); !errors.Is(err, ErrAppNameRequired) {
		t.Fatalf("expected ErrAppNameRequired, got %v", err)
	}
	if _, err := client.Search(context.Background(), "app", nil); !errors.Is(err, ErrSearchRequestRequired) {
		t.Fatalf("expected ErrSearchRequestRequired, got %v", err)
	}
	if _, err := client.Suggest(nil, "app", "", &SuggestRequest{Query: "x"}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Suggest(context.Background(), "app", "", &SuggestRequest{Query: "x"}); !errors.Is(err, ErrModelNameRequired) {
		t.Fatalf("expected ErrModelNameRequired, got %v", err)
	}
	if _, err := client.Hint(nil, "app", &HintRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.HotSearch(nil, "app", &HotSearchRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Request(nil, "GET", "/path", nil, nil, nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := client.Request(context.Background(), "", "/path", nil, nil, nil); !errors.Is(err, ErrRequestMethodRequired) {
		t.Fatalf("expected ErrRequestMethodRequired, got %v", err)
	}
	if _, err := client.Request(context.Background(), "GET", "", nil, nil, nil); !errors.Is(err, ErrRequestPathRequired) {
		t.Fatalf("expected ErrRequestPathRequired, got %v", err)
	}
}

type fakeRequester struct {
	last    requestSpec
	handler func(requestSpec) map[string]any
}

func (f *fakeRequester) Do(_ context.Context, req requestSpec) (map[string]any, error) {
	f.last = req
	if f.handler != nil {
		return f.handler(req), nil
	}
	return map[string]any{}, nil
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
