# elasticsearchx

`elasticsearchx` 是基于官方 `go-elasticsearch/v9` 的强类型封装，简化了复杂的 API 调用，并提供了类型安全的接口。

## ✨ 特性

*   **强类型接口**：封装了 CreateIndex, Search, Index, Get, Update, Delete 等常用操作，避免手动拼接 JSON。
*   **配置简化**：统一配置入口，支持 Basic Auth, API Key, Elastic Cloud。
*   **Header 管理**：自动处理 `application/json` 等 Header，兼容 ES 9.x typed client 约定。
*   **双客户端模式**：同时暴露 `TypedClient` (类型化) 和 `LowLevelClient` (低级) 以满足不同需求。

## 🚀 快速开始

### 初始化

```go
import "github.com/bang-go/micro/store/elasticsearchx"

client, err := elasticsearchx.New(&elasticsearchx.Config{
    Addresses: []string{"http://localhost:9200"},
    Username:  "elastic",
    Password:  "password",
})
```

### 创建索引

```go
mapping := map[string]interface{}{
    "mappings": map[string]interface{}{
        "properties": map[string]interface{}{
            "title": map[string]interface{}{"type": "text"},
            "price": map[string]interface{}{"type": "integer"},
        },
    },
}
_, err := client.CreateIndex(ctx, "products", mapping)
```

### 搜索文档 (类型化)

```go
import (
    "github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
    "github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

req := &search.Request{
    Query: &types.Query{
        Match: map[string]types.MatchQuery{
            "title": {Query: "iphone"},
        },
    },
}
res, err := client.Search(ctx, "products", req)
fmt.Println("Hits:", res.Hits.Total.Value)
```

## ⚙️ 配置说明

```go
type Config struct {
    Addresses []string
    Username  string
    Password  string
    APIKey    string
    CloudID   string
    CACert    []byte
}
```
