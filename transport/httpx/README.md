# httpx

`httpx` 是面向生产环境的 `net/http` 封装。它保留原生 HTTP 语义，不引入导入时副作用，并把客户端与服务端生命周期、观测与错误边界明确拆开。

## 设计原则

- 客户端和服务端配置分离，避免一个 `Config` 同时管理两种完全不同的生命周期。
- 请求与响应使用接近原生 `net/http` 的无损模型，完整保留多值 Header、多 Cookie、Query 合并与显式 Body 语义。
- 不在 `init()` 中注册 Prometheus 指标，指标统一按需懒注册。
- 所有显式接收 `context.Context` 的公开方法都要求非 nil context，不静默回退到 `context.Background()`。
- 不修改调用方传入的 `*http.Client` / `*http.Transport`，内部总是克隆后再包裹观测逻辑。
- 服务端具备显式 `Start` / `Serve` / `Shutdown` / `Close` 生命周期和 panic recovery。

## Client

```go
client := httpx.NewClient(&httpx.ClientConfig{
    Timeout:      5 * time.Second,
    Trace:        true,
    EnableLogger: true,
})

req := &httpx.Request{
    Method: httpx.MethodPost,
    URL:    "https://api.example.com/users?source=micro",
    Query:  url.Values{"expand": {"profile"}},
    Header: http.Header{
        "X-Request-ID": {"req-123"},
    },
}

if err := req.SetJSONBody(map[string]any{
    "name": "alice",
}); err != nil {
    panic(err)
}

resp, err := client.Do(context.Background(), req)
if err != nil {
    panic(err)
}

fmt.Println(resp.StatusCode, resp.Text())
```

`Response` 会完整保留 `Header`、`Cookies`、`Body`，并提供：

- `Success() bool`
- `Text() string`
- `DecodeJSON(dst any) error`

指标默认注册到 `prometheus.DefaultRegisterer`；如果你需要隔离 registry，可以通过 `MetricsRegisterer` 注入，或者用 `DisableMetrics` 完全关闭。

## Server

```go
server := httpx.NewServer(&httpx.ServerConfig{
    Addr:         ":8080",
    Trace:        true,
    EnableLogger: true,
})

handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", httpx.ContentTypeJSON)
    w.WriteHeader(http.StatusOK)
    _, _ = w.Write([]byte(`{"ok":true}`))
})

go func() {
    if err := server.Start(context.Background(), handler); err != nil {
        panic(err)
    }
}()

defer server.Shutdown(context.Background())
```

默认行为：

- 自动暴露健康检查 `GET /healthz`
- panic recovery 返回 `500 Internal Server Error`
- 只有框架内置的 `/healthz` 和 `/metrics` 默认跳过 trace / metrics / access log；禁用健康检查后，你自己的 `/healthz` 不会被静默跳过
- `Request.Build` / `Client.Do` / `Server.Start` / `Server.Serve` / `Server.Shutdown` 都要求非 nil context
- `Shutdown` 在没有 deadline 时会自动应用 `ShutdownTimeout`

如果你不希望框架接管健康检查，可以设置：

```go
server := httpx.NewServer(&httpx.ServerConfig{
    Addr:                  ":8080",
    DisableHealthEndpoint: true,
})
```

## API 摘要

```go
type Client interface {
    Do(context.Context, *Request) (*Response, error)
    HTTPClient() *http.Client
    CloseIdleConnections()
}

type Server interface {
    Start(context.Context, http.Handler) error
    Serve(context.Context, net.Listener, http.Handler) error
    Shutdown(context.Context) error
    Close() error
    HTTPServer() *http.Server
}
```
