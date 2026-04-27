package polarsearchx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	opensearch "github.com/opensearch-project/opensearch-go/v2"
	"github.com/opensearch-project/opensearch-go/v2/opensearchapi"
)

const (
	defaultTimeout    = 10 * time.Second
	defaultMaxRetries = 2
)

var (
	ErrNilConfig             = errors.New("polarsearchx: config is required")
	ErrContextRequired       = errors.New("polarsearchx: context is required")
	ErrAddressRequired       = errors.New("polarsearchx: at least one address is required")
	ErrIndexRequired         = errors.New("polarsearchx: index is required")
	ErrBodyRequired          = errors.New("polarsearchx: request body is required")
	ErrRequestMethodRequired = errors.New("polarsearchx: request method is required")
	ErrRequestPathRequired   = errors.New("polarsearchx: request path is required")
)

type Config struct {
	Addresses []string
	Username  string
	Password  string
	Header    http.Header
	Transport http.RoundTripper
	Timeout   time.Duration

	newClient func(opensearch.Config) (*opensearch.Client, error)
}

type Client interface {
	Search(context.Context, string, any) (*SearchResponse, error)
	Raw(context.Context, string, string, any) (*RawResponse, error)
	RawClient() *opensearch.Client
}

type SearchResponse struct {
	Took     int64      `json:"took"`
	TimedOut bool       `json:"timed_out"`
	Hits     SearchHits `json:"hits"`
}

type SearchHits struct {
	Total TotalHits   `json:"total"`
	Hits  []SearchHit `json:"hits"`
}

type TotalHits struct {
	Value    int64  `json:"value"`
	Relation string `json:"relation"`
}

type SearchHit struct {
	Index  string          `json:"_index"`
	ID     string          `json:"_id"`
	Score  *float64        `json:"_score"`
	Source json.RawMessage `json:"_source"`
	Fields json.RawMessage `json:"fields,omitempty"`
	Sort   json.RawMessage `json:"sort,omitempty"`
}

type RawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type ResponseError struct {
	StatusCode int
	Body       []byte
}

func (e *ResponseError) Error() string {
	if e == nil {
		return ""
	}
	body := strings.TrimSpace(string(e.Body))
	if body == "" {
		return fmt.Sprintf("polarsearchx: request failed with status %d", e.StatusCode)
	}
	return fmt.Sprintf("polarsearchx: request failed with status %d: %s", e.StatusCode, body)
}

type client struct {
	raw *opensearch.Client
}

func Open(conf *Config) (Client, error) {
	return New(conf)
}

func New(conf *Config) (Client, error) {
	config, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	raw, err := config.newClient(opensearch.Config{
		Addresses:     config.Addresses,
		Username:      config.Username,
		Password:      config.Password,
		Header:        cloneHeader(config.Header),
		Transport:     config.Transport,
		MaxRetries:    defaultMaxRetries,
		RetryOnStatus: []int{502, 503, 504},
	})
	if err != nil {
		return nil, fmt.Errorf("polarsearchx: create opensearch client failed: %w", err)
	}
	return &client{raw: raw}, nil
}

func (c *client) Search(ctx context.Context, index string, body any) (*SearchResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	index = strings.TrimSpace(index)
	if index == "" {
		return nil, ErrIndexRequired
	}
	reader, err := bodyReader(body)
	if err != nil {
		return nil, err
	}

	resp, err := opensearchapi.SearchRequest{
		Index: []string{index},
		Body:  reader,
	}.Do(ctx, c.raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("polarsearchx: read search response failed: %w", err)
	}
	if resp.IsError() {
		return nil, &ResponseError{StatusCode: resp.StatusCode, Body: data}
	}

	var out SearchResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("polarsearchx: decode search response failed: %w", err)
	}
	return &out, nil
}

func (c *client) Raw(ctx context.Context, method, path string, body any) (*RawResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, ErrRequestMethodRequired
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrRequestPathRequired
	}
	reader, err := optionalBodyReader(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return nil, fmt.Errorf("polarsearchx: create raw request failed: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.raw.Perform(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("polarsearchx: read raw response failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, &ResponseError{StatusCode: resp.StatusCode, Body: data}
	}
	return &RawResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       data,
	}, nil
}

func (c *client) RawClient() *opensearch.Client {
	return c.raw
}

func prepareConfig(conf *Config) (*Config, error) {
	if conf == nil {
		return nil, ErrNilConfig
	}
	config := *conf
	config.Addresses = compactAddresses(config.Addresses)
	if len(config.Addresses) == 0 {
		return nil, ErrAddressRequired
	}
	config.Header = cloneHeader(config.Header)
	if config.Transport == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		config.Transport = withTimeout(http.DefaultTransport, timeout)
	}
	if config.newClient == nil {
		config.newClient = opensearch.NewClient
	}
	return &config, nil
}

func withTimeout(next http.RoundTripper, timeout time.Duration) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		ctx := req.Context()
		if _, ok := ctx.Deadline(); ok {
			return next.RoundTrip(req)
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return next.RoundTrip(req.WithContext(ctx))
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func bodyReader(body any) (io.Reader, error) {
	if body == nil {
		return nil, ErrBodyRequired
	}
	return optionalBodyReader(body)
}

func optionalBodyReader(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	switch value := body.(type) {
	case io.Reader:
		return value, nil
	case []byte:
		return bytes.NewReader(value), nil
	case string:
		return strings.NewReader(value), nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("polarsearchx: encode request body failed: %w", err)
		}
		return bytes.NewReader(data), nil
	}
}

func compactAddresses(addresses []string) []string {
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address != "" {
			out = append(out, address)
		}
	}
	return out
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return nil
	}
	out := make(http.Header, len(header))
	for k, values := range header {
		out[k] = append([]string(nil), values...)
	}
	return out
}
