package elasticsearchx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bang-go/util"
	"github.com/elastic/go-elasticsearch/v9"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/bulk"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/delete"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/get"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/index"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/update"
	"github.com/elastic/go-elasticsearch/v9/typedapi/indices/create"
	indicesdelete "github.com/elastic/go-elasticsearch/v9/typedapi/indices/delete"
	indicesget "github.com/elastic/go-elasticsearch/v9/typedapi/indices/get"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

var (
	ErrNilConfig              = errors.New("elasticsearchx: config is required")
	ErrContextRequired        = errors.New("elasticsearchx: context is required")
	ErrAddressRequired        = errors.New("elasticsearchx: at least one address or cloud id is required")
	ErrIndexRequired          = errors.New("elasticsearchx: index is required")
	ErrDocumentRequired       = errors.New("elasticsearchx: document is required")
	ErrIDRequired             = errors.New("elasticsearchx: document id is required")
	ErrSearchRequestRequired  = errors.New("elasticsearchx: search request is required")
	ErrBulkOperationsRequired = errors.New("elasticsearchx: bulk operations are required")
	ErrBulkActionRequired     = errors.New("elasticsearchx: bulk action is required")
	ErrBulkUpdateDocRequired  = errors.New("elasticsearchx: bulk update doc is required")
	ErrBulkDocumentRequired   = errors.New("elasticsearchx: bulk document is required")
)

type Config struct {
	Addresses []string
	Username  string
	Password  string
	APIKey    string
	CloudID   string
	CACert    []byte
	Header    http.Header
	Transport http.RoundTripper

	newClient      func(elasticsearch.Config) (*elasticsearch.Client, error)
	newTypedClient func(elasticsearch.Config) (*elasticsearch.TypedClient, error)
}

type Client interface {
	CreateIndex(context.Context, string, any) (*create.Response, error)
	GetIndex(context.Context, string) (indicesget.Response, error)
	ExistsIndex(context.Context, string) (bool, error)
	DeleteIndex(context.Context, string) (*indicesdelete.Response, error)

	Index(context.Context, string, string, any) (*index.Response, error)
	Get(context.Context, string, string) (*get.Response, error)
	Update(context.Context, string, string, map[string]any) (*update.Response, error)
	Delete(context.Context, string, string) (*delete.Response, error)
	Search(context.Context, string, *search.Request) (*search.Response, error)
	Bulk(context.Context, []BulkOperation) (*bulk.Response, error)

	Raw() *elasticsearch.TypedClient
	RawLowLevel() *elasticsearch.Client
	GetClient() *elasticsearch.TypedClient
	GetLowLevelClient() *elasticsearch.Client
}

type BulkOperation struct {
	Action   string
	Index    string
	ID       string
	Document any
	Doc      map[string]any
}

type client struct {
	typed *elasticsearch.TypedClient
	raw   *elasticsearch.Client
}

func Open(conf *Config) (Client, error) {
	return New(conf)
}

func New(conf *Config) (Client, error) {
	config, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	esConfig := elasticsearch.Config{
		Addresses: config.Addresses,
		Username:  config.Username,
		Password:  config.Password,
		APIKey:    config.APIKey,
		CloudID:   config.CloudID,
		CACert:    append([]byte(nil), config.CACert...),
		Header:    cloneHeader(config.Header),
		Transport: config.Transport,
	}

	raw, err := config.newClient(esConfig)
	if err != nil {
		return nil, fmt.Errorf("elasticsearchx: create low-level client failed: %w", err)
	}
	typed, err := config.newTypedClient(esConfig)
	if err != nil {
		return nil, fmt.Errorf("elasticsearchx: create typed client failed: %w", err)
	}

	return &client{
		typed: typed,
		raw:   raw,
	}, nil
}

func (c *client) CreateIndex(ctx context.Context, name string, mapping any) (*create.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}

	req := c.typed.Indices.Create(name)
	if mapping != nil {
		body, err := marshalBody(mapping)
		if err != nil {
			return nil, fmt.Errorf("elasticsearchx: encode create index body failed: %w", err)
		}
		req = req.Raw(bytes.NewReader(body))
	}
	return req.Do(ctx)
}

func (c *client) GetIndex(ctx context.Context, name string) (indicesget.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}
	return c.typed.Indices.Get(name).Do(ctx)
}

func (c *client) ExistsIndex(ctx context.Context, name string) (bool, error) {
	if ctx == nil {
		return false, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false, ErrIndexRequired
	}
	return c.typed.Indices.Exists(name).Do(ctx)
}

func (c *client) DeleteIndex(ctx context.Context, name string) (*indicesdelete.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}
	return c.typed.Indices.Delete(name).Do(ctx)
}

func (c *client) Index(ctx context.Context, name, id string, document any) (*index.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}
	if document == nil {
		return nil, ErrDocumentRequired
	}

	req := c.typed.Index(name).Request(document)
	if trimmedID := strings.TrimSpace(id); trimmedID != "" {
		req = req.Id(trimmedID)
	}
	return req.Do(ctx)
}

func (c *client) Get(ctx context.Context, name, id string) (*get.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrIDRequired
	}
	return c.typed.Get(name, id).Do(ctx)
}

func (c *client) Update(ctx context.Context, name, id string, doc map[string]any) (*update.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrIDRequired
	}
	if len(doc) == 0 {
		return nil, ErrDocumentRequired
	}

	body, err := marshalBody(map[string]any{"doc": doc})
	if err != nil {
		return nil, fmt.Errorf("elasticsearchx: encode update body failed: %w", err)
	}
	return c.typed.Update(name, id).Raw(bytes.NewReader(body)).Do(ctx)
}

func (c *client) Delete(ctx context.Context, name, id string) (*delete.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrIDRequired
	}
	return c.typed.Delete(name, id).Do(ctx)
}

func (c *client) Search(ctx context.Context, name string, request *search.Request) (*search.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrIndexRequired
	}
	if request == nil {
		return nil, ErrSearchRequestRequired
	}
	return c.typed.Search().Index(name).Request(request).Do(ctx)
}

func (c *client) Bulk(ctx context.Context, operations []BulkOperation) (*bulk.Response, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if len(operations) == 0 {
		return nil, ErrBulkOperationsRequired
	}

	req := c.typed.Bulk()
	for _, operation := range operations {
		if err := appendBulkOperation(req, operation); err != nil {
			return nil, err
		}
	}

	return req.Do(ctx)
}

func (c *client) Raw() *elasticsearch.TypedClient {
	return c.typed
}

func (c *client) RawLowLevel() *elasticsearch.Client {
	return c.raw
}

func (c *client) GetClient() *elasticsearch.TypedClient {
	return c.Raw()
}

func (c *client) GetLowLevelClient() *elasticsearch.Client {
	return c.RawLowLevel()
}

func prepareConfig(conf *Config) (*Config, error) {
	if conf == nil {
		return nil, ErrNilConfig
	}

	cloned := *conf
	cloned.Username = strings.TrimSpace(cloned.Username)
	cloned.Password = strings.TrimSpace(cloned.Password)
	cloned.APIKey = strings.TrimSpace(cloned.APIKey)
	cloned.CloudID = strings.TrimSpace(cloned.CloudID)
	cloned.Addresses = trimAddresses(conf.Addresses)
	cloned.Header = cloneHeader(conf.Header)
	cloned.CACert = append([]byte(nil), conf.CACert...)

	if len(cloned.Addresses) == 0 && cloned.CloudID == "" {
		return nil, ErrAddressRequired
	}

	if cloned.Header == nil {
		cloned.Header = make(http.Header)
	}
	if cloned.Header.Get("Accept") == "" {
		cloned.Header.Set("Accept", "application/json")
	}
	if cloned.Header.Get("Content-Type") == "" {
		cloned.Header.Set("Content-Type", "application/json")
	}

	if cloned.newClient == nil {
		cloned.newClient = elasticsearch.NewClient
	}
	if cloned.newTypedClient == nil {
		cloned.newTypedClient = elasticsearch.NewTypedClient
	}

	return &cloned, nil
}

func appendBulkOperation(req *bulk.Bulk, operation BulkOperation) error {
	indexName := strings.TrimSpace(operation.Index)
	if indexName == "" {
		return ErrIndexRequired
	}

	action := strings.ToLower(strings.TrimSpace(operation.Action))
	if action == "" {
		return ErrBulkActionRequired
	}

	switch action {
	case "index", "create":
		if operation.Document == nil {
			return ErrBulkDocumentRequired
		}
		meta := types.IndexOperation{
			Index_: util.Ptr(indexName),
		}
		if id := strings.TrimSpace(operation.ID); id != "" {
			meta.Id_ = util.Ptr(id)
		}
		if action == "index" {
			return req.IndexOp(meta, operation.Document)
		}
		return req.CreateOp(types.CreateOperation(meta), operation.Document)
	case "update":
		if len(operation.Doc) == 0 {
			return ErrBulkUpdateDocRequired
		}
		id := strings.TrimSpace(operation.ID)
		if id == "" {
			return ErrIDRequired
		}
		body, err := json.Marshal(operation.Doc)
		if err != nil {
			return fmt.Errorf("elasticsearchx: encode bulk update doc failed: %w", err)
		}
		return req.UpdateOp(types.UpdateOperation{
			Index_: util.Ptr(indexName),
			Id_:    util.Ptr(id),
		}, nil, &types.UpdateAction{Doc: body})
	case "delete":
		id := strings.TrimSpace(operation.ID)
		if id == "" {
			return ErrIDRequired
		}
		return req.DeleteOp(types.DeleteOperation{
			Index_: util.Ptr(indexName),
			Id_:    util.Ptr(id),
		})
	default:
		return fmt.Errorf("elasticsearchx: unsupported bulk action %q", operation.Action)
	}
}

func trimAddresses(addresses []string) []string {
	if len(addresses) == 0 {
		return nil
	}
	trimmed := make([]string, 0, len(addresses))
	seen := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		if value := strings.TrimSpace(address); value != "" {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			trimmed = append(trimmed, value)
		}
	}
	return trimmed
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return nil
	}
	cloned := make(http.Header, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func marshalBody(value any) ([]byte, error) {
	return json.Marshal(value)
}
