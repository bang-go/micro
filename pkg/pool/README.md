# pool

`pool` 是一个有界 goroutine 池。它不追求花哨调度策略，核心目标是把并发上限、队列背压、panic 隔离和关闭语义做干净。

## 设计原则

- worker 数固定，队列有界，容量语义明确。
- 默认阻塞提交，优先把背压还给调用方，而不是静默丢任务。
- 需要取消或超时控制时，显式使用 `SubmitContext`。
- worker panic 不会炸掉整个池；可以走自定义 `PanicHandler` 或结构化日志。
- `Release` 幂等，且会等待 worker 退出。

## 快速开始

```go
pool, err := pool.New(
    8,
    pool.WithQueueSize(32),
    pool.WithNonBlocking(false),
)
if err != nil {
    panic(err)
}
defer pool.Release()

if err := pool.SubmitContext(context.Background(), func() {
    fmt.Println("run task")
}); err != nil {
    panic(err)
}
```

## API 摘要

```go
type Pool interface {
    Submit(func()) error
    SubmitContext(context.Context, func()) error
    Release()
    Running() int
    Pending() int
    Cap() int
    IsClosed() bool
}

func New(size int, opts ...Option) (Pool, error)
func WithPanicHandler(func(interface{})) Option
func WithLogger(*logger.Logger) Option
func WithNonBlocking(bool) Option
func WithQueueSize(int) Option
```

## 默认行为

- `Submit` 使用 `context.Background()`；如果你需要取消和超时，请直接用 `SubmitContext`
- `SubmitContext` 现在要求非 nil context
- 默认队列长度等于池大小
- `WithNonBlocking(true)` 时，队列满会直接返回 `ErrPoolFull`
- 没有自定义 panic handler 时，panic 会记日志但不会中断其他 worker
