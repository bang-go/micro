package fusion

import (
	"fmt"

	"github.com/alibabacloud-go/darabonba-openapi/v2/models"
	dypnsapi "github.com/alibabacloud-go/dypnsapi-20170525/v2/client"
	teeUtil "github.com/alibabacloud-go/tea-utils/v2/service"
)

// Config 类型别名定义，简化导入
type Config = models.Config
type Option = teeUtil.RuntimeOptions
type GetFusionAuthTokenRequest = dypnsapi.GetFusionAuthTokenRequest
type GetFusionAuthTokenResponse = dypnsapi.GetFusionAuthTokenResponse
type VerifyWithFusionAuthTokenRequest = dypnsapi.VerifyWithFusionAuthTokenRequest
type VerifyWithFusionAuthTokenResponse = dypnsapi.VerifyWithFusionAuthTokenResponse

// Client 定义了Fusion认证客户端的接口
type Client interface {
	// GetFusionAuthToken 获取Fusion认证Token
	GetFusionAuthToken(*GetFusionAuthTokenRequest) (*GetFusionAuthTokenResponse, error)
	// GetFusionAuthTokenWithOptions 获取Fusion认证Token（带运行时选项）
	GetFusionAuthTokenWithOptions(*GetFusionAuthTokenRequest, *Option) (*GetFusionAuthTokenResponse, error)
	// VerifyWithFusionAuthToken 使用Fusion认证Token进行验证
	VerifyWithFusionAuthToken(*VerifyWithFusionAuthTokenRequest) (*VerifyWithFusionAuthTokenResponse, error)
	// VerifyWithFusionAuthTokenWithOptions 使用Fusion认证Token进行验证（带运行时选项）
	VerifyWithFusionAuthTokenWithOptions(*VerifyWithFusionAuthTokenRequest, *Option) (*VerifyWithFusionAuthTokenResponse, error)
}

// ClientEntity 实现了Client接口
type ClientEntity struct {
	*Config
	fusionClient *dypnsapi.Client
}

// New 创建新的Fusion认证客户端
// config: Fusion 认证配置
// 返回: Client 实例和错误
func New(config *Config) (Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config 不能为 nil")
	}

	client := &ClientEntity{Config: config}
	var err error
	client.fusionClient, err = dypnsapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建 Fusion 客户端失败: %w", err)
	}
	return client, nil
}

// GetFusionAuthToken 获取Fusion认证Token
func (f *ClientEntity) GetFusionAuthToken(request *GetFusionAuthTokenRequest) (*GetFusionAuthTokenResponse, error) {
	return f.fusionClient.GetFusionAuthToken(request)
}

// GetFusionAuthTokenWithOptions 获取Fusion认证Token（带运行时选项）
func (f *ClientEntity) GetFusionAuthTokenWithOptions(request *GetFusionAuthTokenRequest, runtime *Option) (*GetFusionAuthTokenResponse, error) {
	return f.fusionClient.GetFusionAuthTokenWithOptions(request, runtime)
}

// VerifyWithFusionAuthToken 使用Fusion认证Token进行验证
func (f *ClientEntity) VerifyWithFusionAuthToken(request *VerifyWithFusionAuthTokenRequest) (*VerifyWithFusionAuthTokenResponse, error) {
	return f.fusionClient.VerifyWithFusionAuthToken(request)
}

// VerifyWithFusionAuthTokenWithOptions 使用Fusion认证Token进行验证（带运行时选项）
func (f *ClientEntity) VerifyWithFusionAuthTokenWithOptions(request *VerifyWithFusionAuthTokenRequest, runtime *Option) (*VerifyWithFusionAuthTokenResponse, error) {
	return f.fusionClient.VerifyWithFusionAuthTokenWithOptions(request, runtime)
}
