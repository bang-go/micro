package ossx

import (
	"context"
	"fmt"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

// Config 类型别名定义，简化导入
type Config = aliyunoss.Config
type Options = aliyunoss.Options
type AppendOptions = aliyunoss.AppendOptions
type PutObjectRequest = aliyunoss.PutObjectRequest
type PutObjectResult = aliyunoss.PutObjectResult
type AppendObjectRequest = aliyunoss.AppendObjectRequest
type AppendObjectResult = aliyunoss.AppendObjectResult
type AppendOnlyFile = aliyunoss.AppendOnlyFile

// Client 定义了OSS客户端的接口
type Client interface {
	// PutObject 上传对象到OSS
	PutObject(context.Context, *PutObjectRequest, ...func(*Options)) (*PutObjectResult, error)
	// PutObjectFromFile 从本地文件上传对象到OSS
	PutObjectFromFile(context.Context, string, *PutObjectRequest, ...func(*Options)) (*PutObjectResult, error)
	// AppendObject 追加对象到OSS
	AppendObject(context.Context, *AppendObjectRequest, ...func(*Options)) (*AppendObjectResult, error)
	// AppendFile 追加文件到OSS
	AppendFile(context.Context, string, string, ...func(*AppendOptions)) (*AppendOnlyFile, error)
}

// ClientEntity 实现了Client接口
type ClientEntity struct {
	*Config
	ossClient *aliyunoss.Client
}

// New creates a new OSS client.
// config: OSS configuration.
// optFns: Optional configuration functions.
func New(config *Config, optFns ...func(*Options)) (Client, error) {
	if config == nil {
		return nil, fmt.Errorf("ossx: config is required")
	}
	client := &ClientEntity{
		Config: config,
	}
	client.ossClient = aliyunoss.NewClient(config, optFns...)
	return client, nil
}

// NewCredentialsProvider 创建静态凭据提供者
func NewCredentialsProvider(accessKeyId, accessKeySecret string) credentials.CredentialsProvider {
	return credentials.NewStaticCredentialsProvider(accessKeyId, accessKeySecret)
}

// PutObject 上传对象到OSS
func (c *ClientEntity) PutObject(ctx context.Context, req *PutObjectRequest, optFns ...func(*Options)) (*PutObjectResult, error) {
	return c.ossClient.PutObject(ctx, req, optFns...)
}

// PutObjectFromFile 从本地文件上传对象到OSS
func (c *ClientEntity) PutObjectFromFile(ctx context.Context, localFile string, req *PutObjectRequest, optFns ...func(*Options)) (*PutObjectResult, error) {
	return c.ossClient.PutObjectFromFile(ctx, req, localFile, optFns...)
}

// AppendObject 追加对象到OSS
func (c *ClientEntity) AppendObject(ctx context.Context, req *AppendObjectRequest, optFns ...func(*Options)) (*AppendObjectResult, error) {
	return c.ossClient.AppendObject(ctx, req, optFns...)
}

// AppendFile 追加文件到OSS
func (c *ClientEntity) AppendFile(ctx context.Context, bucket string, key string, optFns ...func(*AppendOptions)) (*AppendOnlyFile, error) {
	return c.ossClient.AppendFile(ctx, bucket, key, optFns...)
}
