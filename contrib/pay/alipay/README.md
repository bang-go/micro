# alipay

`alipay` 是对 `go-pay/alipay` 的生产级封装。它保留原始支付能力，但把配置校验、签名模式、通知验签和上下文边界做得更严格。

## 设计原则

- 客户端初始化阶段就校验关键配置，不把明显错误留到线上首单。
- `SignType` 只允许 `RSA` / `RSA2`。
- 运行时支付 API 统一要求非 nil context。
- 通知解析和验签边界明确，必须显式提供验签配置。
- 支持公钥模式和证书模式，两种模式互不混淆。

## 快速开始

```go
client, err := alipay.New(&alipay.Config{
    AppID:           "your-app-id",
    PrivateKey:      "your-private-key",
    NotifyURL:       "https://api.example.com/pay/alipay/notify",
    ReturnURL:       "https://app.example.com/pay/result",
    AlipayPublicKey: "alipay-public-key",
})
if err != nil {
    panic(err)
}

payURL, err := client.TradePagePay(context.Background(), gopay.BodyMap{
    "out_trade_no":  "order-1001",
    "total_amount":  "9.90",
    "subject":       "Bang Order",
    "product_code":  "FAST_INSTANT_TRADE_PAY",
})
if err != nil {
    panic(err)
}

fmt.Println(payURL)
```

如果你使用证书模式：

```go
client, err := alipay.New(&alipay.Config{
    AppID:      "your-app-id",
    PrivateKey: "your-private-key",
    Certificate: &alipay.CertificateConfig{
        AppCertPath:          "/path/appCertPublicKey.crt",
        RootCertPath:         "/path/alipayRootCert.crt",
        AlipayPublicCertPath: "/path/alipayCertPublicKey_RSA2.crt",
    },
})
```

## API 摘要

```go
type Client interface {
    Raw() *gopayalipay.Client
    TradePagePay(context.Context, gopay.BodyMap) (string, error)
    TradeWapPay(context.Context, gopay.BodyMap) (string, error)
    TradeAppPay(context.Context, gopay.BodyMap) (string, error)
    TradePrecreate(context.Context, gopay.BodyMap) (*gopayalipay.TradePrecreateResponse, error)
    TradePay(context.Context, gopay.BodyMap) (*gopayalipay.TradePayResponse, error)
    TradeQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeQueryResponse, error)
    TradeClose(context.Context, gopay.BodyMap) (*gopayalipay.TradeCloseResponse, error)
    TradeRefund(context.Context, gopay.BodyMap) (*gopayalipay.TradeRefundResponse, error)
    TradeRefundQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeFastpayRefundQueryResponse, error)
    TradeBillDownloadQuery(context.Context, gopay.BodyMap) (string, error)
    ParseNotify(*http.Request) (gopay.BodyMap, error)
}

func New(*Config) (Client, error)
```

## 默认行为

- `Charset` 默认 `UTF-8`
- `SignType` 默认 `RSA2`
- `TradeBillDownloadQuery` 在支付宝未返回下载地址时会直接报错
- `ParseNotify` 只有在配置了证书模式或 `AlipayPublicKey` 时才会验签通过
