# umeng

`umeng` 提供友盟推送的生产级封装，聚焦配置校验、显式 `context`、请求清洗、签名和统一错误边界。

## 设计原则

- 初始化阶段校验 `app_key`、`master_secret` 和 endpoint。
- 发送接口统一要求非 nil `context.Context`。
- 发送前会克隆并清洗请求，避免修改调用方对象。
- 保留底层 HTTP client 能力，便于接入自定义 transport、trace 和测试桩。

## 快速开始

```go
client, err := umeng.New(&umeng.Config{
    AppKey:       "app-key",
    MasterSecret: "master-secret",
    AliasType:    "subject_id",
    Production:   true,
})
if err != nil {
    panic(err)
}

_, err = client.Send(context.Background(), &umeng.SendRequest{
    Platform: PlatformAndroid,
    Aliases:  []string{"user-subject-id"},
    Title:    "订单提醒",
    Body:     "订单已送达，请及时查收",
    Extra: map[string]string{
        "link": "app://order/detail?id=1",
    },
})
if err != nil {
    panic(err)
}
```
