# envx

`envx` 是一个极小但严格的环境模式工具包。它只解决两件事：统一识别当前环境，以及在拿不到主机名时提供稳定回退值。

## 设计原则

- 只暴露稳定语义，不接管配置加载流程。
- 环境变量优先级固定为 `APP_ENV` 高于 `GO_ENV`。
- 空白值会被忽略，不把 `"   "` 当成有效环境。
- 常见别名会被规范化为 `dev`、`test`、`prod`。

## 快速开始

```go
mode := envx.Active()

switch mode {
case envx.Development:
    fmt.Println("dev")
case envx.Test:
    fmt.Println("test")
case envx.Production:
    fmt.Println("prod")
default:
    fmt.Println("custom:", mode)
}

fmt.Println(envx.HostnameOr("unknown-host"))
```

## API 摘要

```go
const (
    AppEnvKey = "APP_ENV"
    GoEnvKey  = "GO_ENV"
)

func Active() Mode
func Lookup() (Mode, bool)
func Normalize(string) Mode
func IsDev() bool
func IsTest() bool
func IsProd() bool
func Hostname() string
func HostnameOr(string) string
```

## 默认行为

- `Active()` 在没有环境变量时默认返回 `dev`
- `Normalize("development")`、`Normalize("local")` 都会归一到 `dev`
- `Normalize("testing")`、`Normalize("ci")` 会归一到 `test`
- `Normalize("production")` 会归一到 `prod`
