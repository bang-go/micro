# ossx

`ossx` 是对阿里云 OSS Go SDK v2 的轻量封装。它不改造对象存储模型，而是把配置校验、上下文边界和常见上传操作包得更干净。

## 设计原则

- 继续使用 OSS SDK 的原始请求和选项类型，不重新定义对象上传协议。
- 运行时 API 统一要求非 nil context。
- `Base` 配置会被克隆，避免调用方后续修改污染已构建客户端。
- 支持直接传 `CredentialsProvider`，也支持用 `AccessKeyID` / `AccessKeySecret` 构造默认 provider。
- 文件路径、bucket、key 在边界统一清洗和校验。

## 快速开始

```go
client, err := ossx.New(&ossx.Config{
    Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
    Region:          "cn-hangzhou",
    AccessKeyID:     "ak",
    AccessKeySecret: "sk",
})
if err != nil {
    panic(err)
}

_, err = client.PutObject(context.Background(), &ossx.PutObjectRequest{
    Bucket: util.Ptr("assets"),
    Key:    util.Ptr("hello.txt"),
    Body:   strings.NewReader("hello"),
})
if err != nil {
    panic(err)
}
```

如果你已经有自定义凭证提供器：

```go
client, err := ossx.New(&ossx.Config{
    Endpoint:            "oss-cn-hangzhou.aliyuncs.com",
    Region:              "cn-hangzhou",
    CredentialsProvider: ossx.NewCredentialsProvider("ak", "sk"),
})
```

## API 摘要

```go
type Client interface {
    Raw() *aliyunoss.Client
    PutObject(context.Context, *PutObjectRequest, ...func(*Options)) (*PutObjectResult, error)
    PutObjectFromFile(context.Context, *PutObjectRequest, string, ...func(*Options)) (*PutObjectResult, error)
    AppendObject(context.Context, *AppendObjectRequest, ...func(*Options)) (*AppendObjectResult, error)
    AppendFile(context.Context, string, string, ...func(*AppendOptions)) (*AppendOnlyFile, error)
}

func Open(*Config, ...func(*Options)) (Client, error)
func New(*Config, ...func(*Options)) (Client, error)
func NewCredentialsProvider(string, string) credentials.CredentialsProvider
```

## 默认行为

- `Open(conf, ...)` 和 `New(conf, ...)` 等价
- `Endpoint`、`Region`、`AccessKeyID`、`AccessKeySecret` 会先 `trim`
- `PutObjectFromFile` 会校验并清洗文件路径
- `AppendFile` 会校验并清洗 `bucket` 和 `key`
