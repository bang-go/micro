# wechat

`wechat` 是对微信支付 v3 SDK 的业务向封装。它把证书、客户端、通知处理器和预下单默认值的边界收拢起来，但仍然保留原始 SDK 类型。

## 设计原则

- `New` / `Open` 显式要求 context，用来约束初始化阶段网络和证书相关操作。
- 预下单请求中的 `Appid`、`Mchid`、`NotifyUrl` 可以从配置自动回填。
- 运行时支付 API 统一要求非 nil context。
- 通知解析依赖初始化阶段创建的 verifier / notify handler，不在运行时临时拼装。

## 快速开始

```go
client, err := wechat.New(context.Background(), &wechat.Config{
    AppID:                      "wx123",
    MchID:                      "1900000109",
    MchCertificateSerialNumber: "serial-no",
    MchAPIv3Key:                "01234567890123456789012345678901",
    MchPrivateKeyPath:          "/path/apiclient_key.pem",
    NotifyURL:                  "https://api.example.com/pay/wechat/notify",
})
if err != nil {
    panic(err)
}

resp, err := client.JsapiPrepay(context.Background(), jsapi.PrepayRequest{
    Description: core.String("order-1001"),
    OutTradeNo:  core.String("order-1001"),
    Amount: &jsapi.Amount{
        Total: core.Int64(100),
    },
    Payer: &jsapi.Payer{
        Openid: core.String("user-openid"),
    },
    // Appid / Mchid / NotifyUrl 留空时会自动回填 Config 中的默认值
})
if err != nil {
    panic(err)
}

fmt.Println(resp.PrepayId)
```

## API 摘要

```go
type Client interface {
    JsapiPrepay(context.Context, jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error)
    NativePrepay(context.Context, native.PrepayRequest) (*native.PrepayResponse, error)
    AppPrepay(context.Context, app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error)
    H5Prepay(context.Context, h5.PrepayRequest) (*h5.PrepayResponse, error)

    QueryOrderByOutTradeNo(context.Context, string) (*payments.Transaction, error)
    CloseOrder(context.Context, string) error

    Refund(context.Context, refunddomestic.CreateRequest) (*refunddomestic.Refund, error)
    QueryRefund(context.Context, string) (*refunddomestic.Refund, error)

    ParseNotify(*http.Request, any) (*notify.Request, error)

    Raw() *core.Client
    GetClient() *core.Client
}

func Open(context.Context, *Config, ...Option) (Client, error)
func New(context.Context, *Config, ...Option) (Client, error)
func WithHTTPClient(*http.Client) Option
```

## 默认行为

- 预下单请求里的空白字符串指针会被当成“未设置”
- 如果配置里的 `NotifyURL` 也为空，对应预下单字段会被省略，不会传空字符串
- `ParseNotify` 只有在客户端初始化成功、内部 notify handler 就绪后才能使用
