package ossx

import (
	"context"
	"errors"
	"net/http"
	"strings"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/bang-go/util"
)

var (
	ErrNilConfig           = errors.New("ossx: config is required")
	ErrContextRequired     = errors.New("ossx: context is required")
	ErrEndpointRequired    = errors.New("ossx: endpoint is required")
	ErrRegionRequired      = errors.New("ossx: region is required")
	ErrCredentialsRequired = errors.New("ossx: credentials provider or access keys are required")
	ErrRequestRequired     = errors.New("ossx: request is required")
	ErrFilePathRequired    = errors.New("ossx: file path is required")
	ErrBucketRequired      = errors.New("ossx: bucket is required")
	ErrKeyRequired         = errors.New("ossx: object key is required")
)

type Config struct {
	Endpoint            string
	Region              string
	AccessKeyID         string
	AccessKeySecret     string
	CredentialsProvider credentials.CredentialsProvider
	HTTPClient          *http.Client
	Base                *aliyunoss.Config

	newClient func(*aliyunoss.Config, ...func(*Options)) ossAPI
}

type Options = aliyunoss.Options
type AppendOptions = aliyunoss.AppendOptions
type PutObjectRequest = aliyunoss.PutObjectRequest
type PutObjectResult = aliyunoss.PutObjectResult
type AppendObjectRequest = aliyunoss.AppendObjectRequest
type AppendObjectResult = aliyunoss.AppendObjectResult
type AppendOnlyFile = aliyunoss.AppendOnlyFile

type Client interface {
	Raw() *aliyunoss.Client
	PutObject(context.Context, *PutObjectRequest, ...func(*Options)) (*PutObjectResult, error)
	PutObjectFromFile(context.Context, *PutObjectRequest, string, ...func(*Options)) (*PutObjectResult, error)
	AppendObject(context.Context, *AppendObjectRequest, ...func(*Options)) (*AppendObjectResult, error)
	AppendFile(context.Context, string, string, ...func(*AppendOptions)) (*AppendOnlyFile, error)
}

type ossAPI interface {
	PutObject(context.Context, *PutObjectRequest, ...func(*Options)) (*PutObjectResult, error)
	PutObjectFromFile(context.Context, *PutObjectRequest, string, ...func(*Options)) (*PutObjectResult, error)
	AppendObject(context.Context, *AppendObjectRequest, ...func(*Options)) (*AppendObjectResult, error)
	AppendFile(context.Context, string, string, ...func(*AppendOptions)) (*AppendOnlyFile, error)
}

type client struct {
	api ossAPI
	raw *aliyunoss.Client
}

func Open(conf *Config, optFns ...func(*Options)) (Client, error) {
	return New(conf, optFns...)
}

func New(conf *Config, optFns ...func(*Options)) (Client, error) {
	config, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	api := config.newClient(buildSDKConfig(config), optFns...)
	raw, _ := api.(*aliyunoss.Client)
	return &client{
		api: api,
		raw: raw,
	}, nil
}

func NewCredentialsProvider(accessKeyID, accessKeySecret string) credentials.CredentialsProvider {
	return credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret)
}

func (c *client) Raw() *aliyunoss.Client {
	return c.raw
}

func (c *client) PutObject(ctx context.Context, req *PutObjectRequest, optFns ...func(*Options)) (*PutObjectResult, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if req == nil {
		return nil, ErrRequestRequired
	}
	return c.api.PutObject(ctx, req, optFns...)
}

func (c *client) PutObjectFromFile(ctx context.Context, req *PutObjectRequest, filePath string, optFns ...func(*Options)) (*PutObjectResult, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if req == nil {
		return nil, ErrRequestRequired
	}
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil, ErrFilePathRequired
	}
	return c.api.PutObjectFromFile(ctx, req, filePath, optFns...)
}

func (c *client) AppendObject(ctx context.Context, req *AppendObjectRequest, optFns ...func(*Options)) (*AppendObjectResult, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if req == nil {
		return nil, ErrRequestRequired
	}
	return c.api.AppendObject(ctx, req, optFns...)
}

func (c *client) AppendFile(ctx context.Context, bucket, key string, optFns ...func(*AppendOptions)) (*AppendOnlyFile, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	bucket = strings.TrimSpace(bucket)
	key = strings.TrimSpace(key)
	if bucket == "" {
		return nil, ErrBucketRequired
	}
	if key == "" {
		return nil, ErrKeyRequired
	}
	return c.api.AppendFile(ctx, bucket, key, optFns...)
}

func prepareConfig(conf *Config) (*Config, error) {
	if conf == nil {
		return nil, ErrNilConfig
	}

	cloned := *conf
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.Region = strings.TrimSpace(cloned.Region)
	cloned.AccessKeyID = strings.TrimSpace(cloned.AccessKeyID)
	cloned.AccessKeySecret = strings.TrimSpace(cloned.AccessKeySecret)

	if cloned.newClient == nil {
		cloned.newClient = func(cfg *aliyunoss.Config, optFns ...func(*Options)) ossAPI {
			return aliyunoss.NewClient(cfg, optFns...)
		}
	}

	if cloned.Base != nil {
		cloned.Base = util.Ptr(cloned.Base.Copy())
	}

	sdkConfig := buildSDKConfig(&cloned)
	if sdkConfig.Endpoint == nil || strings.TrimSpace(*sdkConfig.Endpoint) == "" {
		return nil, ErrEndpointRequired
	}
	if sdkConfig.Region == nil || strings.TrimSpace(*sdkConfig.Region) == "" {
		return nil, ErrRegionRequired
	}
	if sdkConfig.CredentialsProvider == nil {
		return nil, ErrCredentialsRequired
	}

	return &cloned, nil
}

func buildSDKConfig(conf *Config) *aliyunoss.Config {
	var sdkConfig aliyunoss.Config
	if conf.Base != nil {
		sdkConfig = conf.Base.Copy()
	} else {
		sdkConfig = *aliyunoss.NewConfig()
	}

	if conf.Endpoint != "" {
		sdkConfig.WithEndpoint(conf.Endpoint)
	}
	if conf.Region != "" {
		sdkConfig.WithRegion(conf.Region)
	}
	if conf.HTTPClient != nil {
		sdkConfig.WithHttpClient(conf.HTTPClient)
	}
	if conf.CredentialsProvider != nil {
		sdkConfig.WithCredentialsProvider(conf.CredentialsProvider)
	} else if conf.AccessKeyID != "" && conf.AccessKeySecret != "" {
		sdkConfig.WithCredentialsProvider(NewCredentialsProvider(conf.AccessKeyID, conf.AccessKeySecret))
	}

	return &sdkConfig
}
