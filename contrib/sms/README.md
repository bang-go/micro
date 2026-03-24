# sms

`sms` 是对阿里云短信 SDK 的业务向封装。它把配置、请求校验和上下文边界整理干净，同时保留原始请求与响应类型。

## 设计原则

- 直接复用阿里云 SDK 的请求和响应类型，不重新建模。
- 请求在发送前会先校验、克隆、清洗，避免调用方对象被内部修改。
- 所有运行时方法统一要求非 nil context。
- 保留带 `RuntimeOptions` 的重载，复杂场景仍可下沉到 SDK 能力。

## 快速开始

```go
client, err := sms.New(&sms.Config{
    AccessKeyID:     "ak",
    AccessKeySecret: "sk",
    Endpoint:        "dysmsapi.aliyuncs.com",
})
if err != nil {
    panic(err)
}

resp, err := client.SendSms(context.Background(), &sms.SendSmsRequest{
    PhoneNumbers: util.Ptr("13800000000"),
    SignName:     util.Ptr("Bang"),
    TemplateCode: util.Ptr("SMS_123456789"),
    TemplateParam: util.Ptr(`{"code":"9527"}`),
})
if err != nil {
    panic(err)
}

fmt.Println(resp.Body)
```

## API 摘要

```go
type Client interface {
    Raw() *dysmsapi.Client
    SendSms(context.Context, *SendSmsRequest) (*SendSmsResponse, error)
    SendSmsWithOptions(context.Context, *SendSmsRequest, *Option) (*SendSmsResponse, error)
    SendBatchSms(context.Context, *SendBatchSmsRequest) (*SendBatchSmsResponse, error)
    SendBatchSmsWithOptions(context.Context, *SendBatchSmsRequest, *Option) (*SendBatchSmsResponse, error)
    QuerySendDetails(context.Context, *QuerySendDetailsRequest) (*QuerySendDetailsResponse, error)
    QuerySendDetailsWithOptions(context.Context, *QuerySendDetailsRequest, *Option) (*QuerySendDetailsResponse, error)
}

func Open(*Config) (Client, error)
func New(*Config) (Client, error)
```

## 默认行为

- `Open(conf)` 和 `New(conf)` 等价
- `PhoneNumbers`、`SignName`、`TemplateCode` 等关键字符串会先 `trim`
- 请求缺少关键字段会在进入 SDK 前直接报错
