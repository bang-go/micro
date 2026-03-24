# opensearchx

`opensearchx` 是对阿里云 OpenSearch 查询接口的干净封装。它提供可组合的查询构造器、统一请求入口和类型化响应解析，同时保留底层 API 的表达能力。

## 设计原则

- 配置严格校验，`Protocol` 只接受 `http` / `https`。
- 查询参数由结构化 builder 生成，避免业务层手拼复杂 query 字符串。
- 运行时 API 统一要求非 nil context。
- 自定义 `Request` 入口会克隆 query / headers map，避免调用方后续修改产生隐藏副作用。

## 快速开始

```go
type Product struct {
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}

client, err := opensearchx.New(&opensearchx.Config{
    Endpoint:        "opensearch-cn-hangzhou.aliyuncs.com",
    Protocol:        "https",
    AccessKeyID:     "ak",
    AccessKeySecret: "sk",
})
if err != nil {
    panic(err)
}

resp, err := opensearchx.SearchTyped[Product](client, context.Background(), "catalog", &opensearchx.SearchRequest{
    Query: &opensearchx.QueryClause{
        Index: "default",
        Value: "iphone",
    },
    Filter: &opensearchx.FilterClause{
        Field:    "status",
        Operator: "=",
        Value:    1,
    },
    Sort: &opensearchx.SortClause{
        Field: "price",
        Order: "-",
    },
    Hit: 20,
})
if err != nil {
    panic(err)
}

fmt.Println(resp.Body.Result.Items)
```

## API 摘要

```go
type Client interface {
    Search(context.Context, string, *SearchRequest) (map[string]any, error)
    Suggest(context.Context, string, string, *SuggestRequest) (*SuggestResponse, error)
    Hint(context.Context, string, *HintRequest) (*HintResponse, error)
    HotSearch(context.Context, string, *HotSearchRequest) (*HotSearchResponse, error)
    Request(context.Context, string, string, map[string]string, map[string]string, any) (map[string]any, error)
}

func Open(*Config) (Client, error)
func New(*Config) (Client, error)
func SearchTyped[T any](Client, context.Context, string, *SearchRequest) (*SearchResponse[T], error)
```

## 默认行为

- 默认 `Protocol` 是 `https`
- 默认连接超时 `5s`
- 默认读超时 `10s`
- 默认最大空闲连接数 `50`
- `SearchRequest` 里的 `Hit <= 0` 时默认 `10`
- `SearchRequest` 里的 `Format` 为空时默认 `fulljson`
