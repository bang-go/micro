# jwtx

`jwtx` 是一个泛型 JWT 封装，目标是把签发和解析常用路径做干净，同时明确限制签名算法和 claim 时间线。

## 设计原则

- 只接受标准 HMAC 算法，避免把非对称签名、公私钥管理和这个轻量封装混在一起。
- `Issuer`、`Audience` 在初始化时统一规范化。
- 签发时校验 `IssuedAt`、`NotBefore`、`ExpiresAt` 的时间关系。
- 默认值明确：方法默认 `HS256`，过期时间默认 `24h`。

## 快速开始

```go
type UserPayload struct {
    UserID int64 `json:"user_id"`
}

j, err := jwtx.New[UserPayload](&jwtx.Config{
    SecretKey: "super-secret",
    Issuer:    "bang-api",
    Audience:  []string{"mobile", "admin"},
})
if err != nil {
    panic(err)
}

token, err := j.Generate(
    UserPayload{UserID: 1001},
    jwtx.WithSubject("user:1001"),
    jwtx.WithJWTID("token-1"),
)
if err != nil {
    panic(err)
}

claims, err := j.Parse(token)
if err != nil {
    panic(err)
}

fmt.Println(claims.Payload.UserID)
```

## API 摘要

```go
func New[T any](*Config) (*JWT[T], error)
func MustNew[T any](*Config) *JWT[T]

func (j *JWT[T]) Generate(payload T, opts ...IssueOption) (string, error)
func (j *JWT[T]) Parse(token string) (*Claims[T], error)
func (j *JWT[T]) ParsePayload(token string) (T, error)

func WithSubject(string) IssueOption
func WithAudience(...string) IssueOption
func WithJWTID(string) IssueOption
func WithNotBefore(time.Time) IssueOption
func WithIssuedAt(time.Time) IssueOption
func WithExpiresAt(time.Time) IssueOption
```

## 默认行为

- 默认签名方法是 `HS256`
- 默认过期时间是 `24h`
- `Audience` 会被去重并移除空白值
- 只允许 `HS256`、`HS384`、`HS512`
