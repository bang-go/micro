package discovery

import (
	"fmt"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type Config vo.NacosClientParam

// New 创建新的 Nacos 服务发现客户端
// conf: Nacos 客户端配置
// 返回: INamingClient 实例和错误
// 文档地址: https://github.com/nacos-group/nacos-sdk-go
func New(conf *Config) (naming_client.INamingClient, error) {
	if conf == nil {
		return nil, fmt.Errorf("Config 不能为 nil")
	}
	if conf.ClientConfig == nil {
		return nil, fmt.Errorf("ClientConfig 不能为 nil")
	}
	if len(conf.ServerConfigs) == 0 {
		return nil, fmt.Errorf("ServerConfigs 不能为空")
	}

	client, err := clients.NewNamingClient(vo.NacosClientParam{
		ClientConfig:  conf.ClientConfig,
		ServerConfigs: conf.ServerConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 服务发现客户端失败: %w", err)
	}
	return client, nil
}
