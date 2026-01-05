# Micro Framework Monitoring Guide

这份指南旨在帮助您在 Grafana 中配置基于 `bang-go/micro` 框架的高价值监控面板。

我们遵循 Google SRE 的 **Golden Signals (黄金指标)** 方法论：**Traffic (流量)**, **Latency (延迟)**, **Errors (错误)**, **Saturation (饱和度)**。

---

## 1. HTTP 服务端 (Ginx)

### 核心指标
- **QPS (流量)**: 每秒请求数，反映业务负载。
- **Latency P99 (延迟)**: 99% 的请求都在多少时间内完成，反映用户体验。
- **Error Rate (错误)**: 5xx 错误占比，反映服务可用性。
- **In-Flight (饱和度)**: 当前正在处理的请求数，反映系统积压情况。

### Grafana Panel 配置

#### A. QPS (Requests Per Second)
*   **Visualization**: Time Series
*   **PromQL**:
    ```promql
    sum(rate(http_server_requests_total[1m])) by (method, path)
    ```
*   **Legend**: `{{method}} {{path}}`

#### B. Latency (P99 & P95)
*   **Visualization**: Time Series
*   **Unit**: Seconds (s)
*   **PromQL**:
    ```promql
    histogram_quantile(0.99, sum(rate(http_server_request_duration_seconds_bucket[1m])) by (le, path))
    ```
    *(添加第二个 Query 修改 quantile 为 0.95)*

#### C. Error Rate (5xx %)
*   **Visualization**: Stat 或 Time Series
*   **Thresholds**: Green < 1%, Red > 5%
*   **PromQL**:
    ```promql
    sum(rate(http_server_requests_total{status=~"5.."}[1m])) 
    / 
    sum(rate(http_server_requests_total[1m])) * 100
    ```

#### D. Saturation (Concurrency)
*   **Visualization**: Gauge 或 Time Series
*   **PromQL**:
    ```promql
    sum(http_server_requests_in_flight) by (path)
    ```
*   **警报建议**: 如果此指标持续超过 CPU 核数 * 10，建议告警。

---

## 2. gRPC 服务端 (Grpcx)

### Grafana Panel 配置

#### A. gRPC QPS & Status
*   **Visualization**: Time Series
*   **PromQL**:
    ```promql
    sum(rate(grpc_server_requests_total[1m])) by (method, code)
    ```
*   **关注点**: 非 `OK` 的状态码，如 `Unavailable` (过载) 或 `DeadlineExceeded` (超时)。

#### B. gRPC Latency
*   **Visualization**: Time Series
*   **PromQL**:
    ```promql
    histogram_quantile(0.99, sum(rate(grpc_server_request_duration_seconds_bucket[1m])) by (le, method))
    ```

#### C. gRPC Saturation
*   **Visualization**: Time Series
*   **PromQL**:
    ```promql
    sum(grpc_server_requests_in_flight) by (method)
    ```

---

## 3. WebSocket (Wsx)

### 核心指标
对于长连接服务，关注点在于**连接保持**和**消息吞吐**。

### Grafana Panel 配置

#### A. Active Connections (在线人数)
*   **Visualization**: Stat + Sparkline
*   **PromQL**:
    ```promql
    sum(ws_connections_active)
    ```

#### B. Message Throughput (消息吞吐量)
*   **Visualization**: Time Series
*   **PromQL (Received)**:
    ```promql
    sum(rate(ws_messages_received_total[1m]))
    ```
*   **PromQL (Sent)**:
    ```promql
    sum(rate(ws_messages_sent_total[1m])) by (status)
    ```

#### C. Dropped Messages (丢包率)
*   **Visualization**: Time Series (Bar Chart)
*   **Color**: Red
*   **PromQL**:
    ```promql
    sum(rate(ws_messages_sent_total{status="dropped"}[1m]))
    ```
*   **含义**: 客户端接收太慢或网络拥堵，导致服务端发送缓冲区满而丢包。需重点关注。

---

## 4. 外部依赖 (Httpx Client)

监控您的服务**调用下游**的情况。

#### A. External API Latency
*   **PromQL**:
    ```promql
    histogram_quantile(0.99, sum(rate(httpx_client_request_duration_seconds_bucket[1m])) by (le, host))
    ```
*   **作用**: 当接口变慢时，快速判断是自身代码问题还是下游服务(host)的问题。

---

## 5. 告警规则建议 (Alerting)

建议配置以下 Prometheus Alert Rules：

1.  **HighErrorRate**: 5xx 错误率 > 5% 持续 5分钟。
2.  **HighLatency**: P99 延迟 > 1秒 持续 5分钟。
3.  **ServiceDown**: `up == 0`。
4.  **WebSocketDropSpike**: WebSocket 丢包率显著上升。
