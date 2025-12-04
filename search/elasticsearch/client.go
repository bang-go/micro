package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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

// Config Elasticsearch 客户端配置
type Config struct {
	// Addresses Elasticsearch 服务器地址列表，例如: []string{"http://localhost:9200"}
	Addresses []string
	// Username 用户名（可选）
	Username string
	// Password 密码（可选）
	Password string
	// APIKey API 密钥（可选，用于 Elastic Cloud）
	APIKey string
	// CloudID Cloud ID（可选，用于 Elastic Cloud）
	CloudID string
	// CACert CA 证书内容（可选，用于 TLS）
	CACert []byte
	// Header 自定义请求头（可选）
	// 注意：类型化 API 的 Header 设置会覆盖客户端级别的 Header 配置
	// 如果需要自定义 Header，建议通过 GetClient() 获取底层客户端自行设置
	Header map[string]string
}

// Client Elasticsearch 客户端接口
type Client interface {
	// CreateIndex ========== 索引操作 ==========
	// CreateIndex 创建索引（返回结构化类型）
	CreateIndex(index string, mapping map[string]interface{}) (*create.Response, error)
	// GetIndex 获取索引信息（返回结构化类型）
	GetIndex(index string) (indicesget.Response, error)
	// ExistsIndex 检查索引是否存在
	ExistsIndex(index string) (bool, error)
	// DeleteIndex 删除索引（返回结构化类型）
	DeleteIndex(index string) (*indicesdelete.Response, error)

	// Index ========== 文档操作 ==========
	// Index 索引文档（增，返回结构化类型）
	Index(index, id string, document interface{}) (*index.Response, error)
	// Get 获取文档（查，返回结构化类型）
	Get(index, id string) (*get.Response, error)
	// Update 更新文档（改，返回结构化类型）
	Update(index, id string, doc map[string]interface{}) (*update.Response, error)
	// Delete 删除文档（删，返回结构化类型）
	Delete(index, id string) (*delete.Response, error)
	// Search 搜索文档（类型化 API，支持 context）
	// 使用完全类型化 API，提供类型安全性和完整功能支持
	Search(ctx context.Context, index string, request *search.Request) (*search.Response, error)
	// Bulk 批量操作（返回结构化类型）
	Bulk(operations []BulkOperation) (*bulk.Response, error)

	// GetClient ========== 高级操作 ==========
	// GetClient 获取底层客户端（用于高级操作）
	GetClient() *elasticsearch.TypedClient
	// GetLowLevelClient 获取底层低级别客户端
	GetLowLevelClient() *elasticsearch.Client
}

// BulkOperation 批量操作
type BulkOperation struct {
	// Action 操作类型: index, create, update, delete
	Action string
	// Index 索引名称
	Index string
	// ID 文档 ID（可选，用于 index, create, update, delete）
	ID string
	// Document 文档内容（用于 index, create, update）
	Document interface{}
	// Doc 更新内容（用于 update）
	Doc map[string]interface{}
}

// ClientEntity 实现了 Client 接口
type ClientEntity struct {
	config         *Config
	typedClient    *elasticsearch.TypedClient
	lowLevelClient *elasticsearch.Client
}

// New 创建新的 Elasticsearch 客户端
// config: Elasticsearch 配置
// 返回: Client 实例和错误
// 使用示例：
//
//	// 本地连接
//	client, err := New(&Config{
//	    Addresses: []string{"http://localhost:9200"},
//	})
//
//	// 使用用户名密码
//	client, err := New(&Config{
//	    Addresses: []string{"http://localhost:9200"},
//	    Username:  "elastic",
//	    Password:  "password",
//	})
//
//	// Elastic Cloud 连接
//	client, err := New(&Config{
//	    CloudID: "your-cloud-id",
//	    APIKey:  "your-api-key",
//	})
func New(config *Config) (Client, error) {
	if config == nil {
		return nil, errors.New("config 不能为 nil")
	}
	if len(config.Addresses) == 0 && config.CloudID == "" {
		return nil, errors.New("Addresses 或 CloudID 必须设置一个")
	}

	cfg := elasticsearch.Config{}

	// 设置地址
	if len(config.Addresses) > 0 {
		cfg.Addresses = config.Addresses
	}

	// 设置 CloudID（用于 Elastic Cloud）
	if config.CloudID != "" {
		cfg.CloudID = config.CloudID
	}

	// 设置认证
	if config.APIKey != "" {
		cfg.APIKey = config.APIKey
	} else if config.Username != "" && config.Password != "" {
		cfg.Username = config.Username
		cfg.Password = config.Password
	}

	// 设置 CA 证书
	if len(config.CACert) > 0 {
		cfg.CACert = config.CACert
	}

	// 设置请求头
	// 默认使用 application/json，解决 media_type_header_exception 错误
	// Elasticsearch 8.0+ 要求明确指定 Content-Type 和 Accept 为 application/json
	// 如果用户自定义了 Header，会与默认值合并（用户自定义的优先级更高）
	cfg.Header = http.Header{
		"Content-Type": []string{"application/json"},
		"Accept":       []string{"application/json"},
	}
	// 如果用户自定义了 Header，合并到默认 Header 中（用户自定义的会覆盖默认值）
	if len(config.Header) > 0 {
		for k, v := range config.Header {
			cfg.Header[k] = []string{v}
		}
	}

	// 创建低级别客户端
	lowLevelClient, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 Elasticsearch 客户端失败: %w", err)
	}

	// 创建类型化客户端（使用相同的配置，包括 Header）
	// 类型化 API 在某些服务器上也需要正确的 Header 设置
	typedClient, err := elasticsearch.NewTypedClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 Elasticsearch 类型化客户端失败: %w", err)
	}

	return &ClientEntity{
		config:         config,
		typedClient:    typedClient,
		lowLevelClient: lowLevelClient,
	}, nil
}

// CreateIndex 创建索引
// 使用完全类型化 API：typedClient.Indices.Create(index).Request(&create.Request{...}).Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) CreateIndex(index string, mapping map[string]interface{}) (*create.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}

	req := &create.Request{}
	if mapping != nil {
		// 将 mapping 转换为 JSON 再解析为类型化结构
		mappingBytes, err := json.Marshal(mapping)
		if err != nil {
			return nil, fmt.Errorf("序列化 mapping 失败: %w", err)
		}
		if err := json.Unmarshal(mappingBytes, req); err != nil {
			// 如果解析失败，使用 Raw 方法传递原始 JSON
			resp, err := c.typedClient.Indices.Create(index).
				Raw(bytes.NewReader(mappingBytes)).
				Header("Content-Type", "application/json").
				Header("Accept", "application/json").
				Do(context.Background())
			return resp, err
		}
	}

	resp, err := c.typedClient.Indices.Create(index).
		Request(req).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("创建索引失败: %w", err)
	}

	return resp, nil
}

// GetIndex 获取索引信息
// 使用完全类型化 API：typedClient.Indices.Get(index).Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) GetIndex(index string) (indicesget.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}

	resp, err := c.typedClient.Indices.Get(index).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("获取索引信息失败: %w", err)
	}

	return resp, nil
}

// ExistsIndex 检查索引是否存在
// 使用完全类型化 API：typedClient.Indices.Exists(index).Do(ctx)
func (c *ClientEntity) ExistsIndex(index string) (bool, error) {
	if index == "" {
		return false, errors.New("index 不能为空")
	}

	exists, err := c.typedClient.Indices.Exists(index).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return false, fmt.Errorf("检查索引是否存在失败: %w", err)
	}

	return exists, nil
}

// DeleteIndex 删除索引
// 使用完全类型化 API：typedClient.Indices.Delete(index).Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) DeleteIndex(index string) (*indicesdelete.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}

	resp, err := c.typedClient.Indices.Delete(index).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("删除索引失败: %w", err)
	}

	return resp, nil
}

// Index 索引文档
// 使用完全类型化 API：typedClient.Index(index).Id(id).Document(document).Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) Index(index, id string, document interface{}) (*index.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}

	req := c.typedClient.Index(index).
		Document(document).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json")

	if id != "" {
		req = req.Id(id)
	}

	resp, err := req.Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("索引文档失败: %w", err)
	}

	return resp, nil
}

// Get 获取文档
// 使用完全类型化 API：typedClient.Get(index, id).Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) Get(index, id string) (*get.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}
	if id == "" {
		return nil, errors.New("id 不能为空")
	}

	resp, err := c.typedClient.Get(index, id).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("获取文档失败: %w", err)
	}

	if !resp.Found {
		return nil, fmt.Errorf("文档不存在: index=%s, id=%s", index, id)
	}

	return resp, nil
}

// Update 更新文档
// 使用完全类型化 API：typedClient.Update(index, id).Request(&update.Request{Doc: doc}).Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) Update(index, id string, doc map[string]interface{}) (*update.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}
	if id == "" {
		return nil, errors.New("id 不能为空")
	}

	docBytes, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("序列化更新文档失败: %w", err)
	}

	req := &update.Request{
		Doc: docBytes,
	}

	resp, err := c.typedClient.Update(index, id).
		Request(req).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("更新文档失败: %w", err)
	}

	return resp, nil
}

// Delete 删除文档
// 使用完全类型化 API：typedClient.Delete(index, id).Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) Delete(index, id string) (*delete.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}
	if id == "" {
		return nil, errors.New("id 不能为空")
	}

	resp, err := c.typedClient.Delete(index, id).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("删除文档失败: %w", err)
	}

	return resp, nil
}

// Search 搜索文档（类型化 API）
// 使用官方推荐的方式：typedClient.Search().Index("my_index").Request(&search.Request{...}).Do(context)
// 通过 Header() 方法显式设置 Content-Type 和 Accept 头，解决 media_type_header_exception 错误
// 参考：https://www.elastic.co/docs/reference/elasticsearch/clients/go/getting-started#_searching_documents
func (c *ClientEntity) Search(ctx context.Context, index string, request *search.Request) (*search.Response, error) {
	if index == "" {
		return nil, errors.New("index 不能为空")
	}

	// 使用完全类型化 API，保持类型安全性和代码清晰度
	return c.typedClient.Search().
		Index(index).
		Request(request).
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(ctx)
}

// Bulk 批量操作
// 使用完全类型化 API：typedClient.Bulk().IndexOp/CreateOp/UpdateOp/DeleteOp().Do(ctx)
// 返回结构化类型，提供类型安全性
func (c *ClientEntity) Bulk(operations []BulkOperation) (*bulk.Response, error) {
	if len(operations) == 0 {
		return nil, errors.New("operations 不能为空")
	}

	bulkReq := c.typedClient.Bulk()

	for _, op := range operations {
		switch op.Action {
		case "index":
			indexOp := types.IndexOperation{
				Index_: &op.Index,
			}
			if op.ID != "" {
				indexOp.Id_ = &op.ID
			}
			if err := bulkReq.IndexOp(indexOp, op.Document); err != nil {
				return nil, fmt.Errorf("添加 index 操作失败: %w", err)
			}
		case "create":
			createOp := types.CreateOperation{
				Index_: &op.Index,
			}
			if op.ID != "" {
				createOp.Id_ = &op.ID
			}
			if err := bulkReq.CreateOp(createOp, op.Document); err != nil {
				return nil, fmt.Errorf("添加 create 操作失败: %w", err)
			}
		case "update":
			updateOp := types.UpdateOperation{
				Index_: &op.Index,
			}
			if op.ID != "" {
				updateOp.Id_ = &op.ID
			}
			updateDoc := map[string]interface{}{}
			if op.Doc != nil {
				updateDoc["doc"] = op.Doc
			} else if op.Document != nil {
				updateDoc["doc"] = op.Document
			}
			updateAction := &types.UpdateAction{}
			if err := bulkReq.UpdateOp(updateOp, updateDoc, updateAction); err != nil {
				return nil, fmt.Errorf("添加 update 操作失败: %w", err)
			}
		case "delete":
			deleteOp := types.DeleteOperation{
				Index_: &op.Index,
			}
			if op.ID != "" {
				deleteOp.Id_ = &op.ID
			}
			if err := bulkReq.DeleteOp(deleteOp); err != nil {
				return nil, fmt.Errorf("添加 delete 操作失败: %w", err)
			}
		default:
			return nil, fmt.Errorf("不支持的操作类型: %s", op.Action)
		}
	}

	resp, err := bulkReq.
		Header("Content-Type", "application/json").
		Header("Accept", "application/json").
		Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("批量操作失败: %w", err)
	}

	return resp, nil
}

// GetClient 获取底层类型化客户端（用于高级操作）
func (c *ClientEntity) GetClient() *elasticsearch.TypedClient {
	return c.typedClient
}

// GetLowLevelClient 获取底层低级别客户端
func (c *ClientEntity) GetLowLevelClient() *elasticsearch.Client {
	return c.lowLevelClient
}
