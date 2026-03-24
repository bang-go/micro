# discovery

`discovery` 是对 Nacos naming client 的薄封装。它不重新抽象注册中心，而是在构造阶段把配置校验、去重和克隆做好。

## 设计原则

- 直接返回 Nacos 原生 `INamingClient`，不发明第二套抽象。
- `ClientConfig` 和 `ServerConfigs` 在边界会被克隆，避免调用方后续修改污染已构建客户端。
- 空白和重复的 `ServerConfig` 会被过滤。
- 必须提供有效 `ServerConfigs` 或 `ClientConfig.Endpoint` 之一。

## 快速开始

```go
clientConfig := constant.NewClientConfig()
clientConfig.NamespaceId = "public"

cli, err := discovery.New(&discovery.Config{
    ClientConfig: clientConfig,
    ServerConfigs: []constant.ServerConfig{
        {IpAddr: "127.0.0.1", Port: 8848},
    },
})
if err != nil {
    panic(err)
}

_ = cli
```

## API 摘要

```go
type Config struct {
    ClientConfig          *constant.ClientConfig
    ServerConfigs         []constant.ServerConfig
    RamCredentialProvider security.RamCredentialProvider
}

func Open(*Config) (naming_client.INamingClient, error)
func New(*Config) (naming_client.INamingClient, error)
```

## 默认行为

- `ClientConfig == nil` 时会使用 `constant.NewClientConfig()` 默认值
- `ServerConfigs` 里的空白项和重复项会被忽略
- 如果最终既没有有效 server config，也没有 `Endpoint`，会直接报错
