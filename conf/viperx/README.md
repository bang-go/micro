# viperx

`viperx` 是基于 `spf13/viper` 的配置加载器封装，专为微服务的多环境配置设计。

## ✨ 特性

*   **多环境支持**：自动根据环境变量 `APP_ENV` 加载对应的配置文件（如 `application.dev.yaml`, `application.prod.yaml`）。
*   **热更新**：支持配置文件修改后的自动热加载 (Watch)。
*   **环境变量覆盖**：支持使用环境变量覆盖配置项（如 `APP_NAME` 覆盖 `app.name`）。
*   **默认配置**：内置合理的默认值，零配置也可启动。

## 🚀 快速开始

### 1. 配置文件 (application.yaml)

```yaml
server:
  port: 8080
  name: demo
```

### 2. 加载配置

```go
import "github.com/bang-go/micro/conf/viperx"

func main() {
    // 加载配置
    v, err := viperx.New(&viperx.Config{
        Name:  "application", // 文件名前缀
        Type:  "yaml",        // 文件类型
        Path:  "./config",    // 路径
        Watch: true,          // 开启热更新
    })
    if err != nil {
        panic(err)
    }

    // 读取配置
    port := v.GetInt("server.port")
    name := v.GetString("server.name")
}
```

### 3. 环境变量覆盖

设置环境变量 `SERVER_PORT=9090` 将自动覆盖配置文件中的 `server.port`。

## ⚙️ 配置说明

```go
type Config struct {
    Name    string // 配置文件名 (默认 "application")
    Type    string // 配置文件类型 (默认 "yaml")
    Path    string // 搜索路径 (默认 ".")
    Watch   bool   // 是否开启热更新
}
```
