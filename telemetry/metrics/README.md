# metrics

Prometheus scrape endpoint helper.

`metrics` 只提供通用运行时采集入口，不定义业务指标，也不复制 Prometheus 的 Counter、Gauge、Histogram API。业务项目需要定义领域指标时，应该在业务项目自己的 infra 包中直接使用 Prometheus 官方类型，并保持标签低基数、无敏感信息。

## 使用

```go
mux.Handle("/metrics", metrics.Handler())
```
