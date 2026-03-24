# viperx

`viperx` 是一个面向微服务的 `viper` 装配层，负责把环境模式、配置文件查找、环境变量覆盖和热更新边界整理干净。

## 设计原则

- 优先尝试环境专属配置文件，例如 `application.dev.yaml`，不存在时再回退到基础文件。
- 环境变量始终可以覆盖文件配置，优先级清晰。
- 配置文件不是强依赖，默认允许纯 ENV 启动；只有显式要求时才因缺文件失败。
- 只有在实际加载到配置文件时才开启 watch，避免空 watch 带来误判。

## 快速开始

```go
cfg, err := viperx.Open(&viperx.Config{
    Name: "application",
    Type: "yaml",
    Paths: []string{"./conf"},
    Watch: true,
    OnChange: func(v *viper.Viper, event fsnotify.Event) {
        fmt.Println("config changed:", event.Name)
    },
})
if err != nil {
    panic(err)
}

port := cfg.GetInt("server.port")
```

## 默认行为

- `Mode` 为空时，自动读取 `envx.Active()`
- `Paths` 为空时，默认使用当前目录 `.`
- `RequireFile` 默认关闭，支持只靠环境变量启动
- `EnvPrefix` 可选；未设置时直接按配置 key 映射环境变量，如 `server.port -> SERVER_PORT`
