package discovery

import (
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type Config vo.NacosClientParam

// New 文档地址:https://github.com/nacos-group/nacos-sdk-go
func New(conf *Config) (naming_client.INamingClient, error) {
	return clients.NewNamingClient(vo.NacosClientParam{ClientConfig: conf.ClientConfig, ServerConfigs: conf.ServerConfigs})
}
