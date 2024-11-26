package config

import (
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

//文档地址:https://github.com/nacos-group/nacos-sdk-go

type ClientConfig = constant.ClientConfig
type ServerConfig = constant.ServerConfig
type Param = vo.ConfigParam
type SearchParam = vo.SearchConfigParam

type clientEntity struct {
	client config_client.IConfigClient
}

// New 新建客户端
func New(clientConf *ClientConfig, serverConf []ServerConfig) (config_client.IConfigClient, error) {
	var err error
	c := &clientEntity{}
	c.client, err = clients.NewConfigClient(vo.NacosClientParam{ClientConfig: clientConf, ServerConfigs: serverConf})
	return c, err
}

// PublishConfig 发布config
func (c *clientEntity) PublishConfig(p Param) (bool, error) {
	return c.client.PublishConfig(p)
}

// DeleteConfig 删除config
func (c *clientEntity) DeleteConfig(p Param) (bool, error) {
	return c.client.DeleteConfig(p)
}

// GetConfig 获取config
func (c *clientEntity) GetConfig(p Param) (string, error) {
	return c.client.GetConfig(p)
}

// ListenConfig 监听config
func (c *clientEntity) ListenConfig(p Param) error {
	return c.client.ListenConfig(p)
}

// CancelListenConfig 取消监听config
func (c *clientEntity) CancelListenConfig(p Param) error {
	return c.client.CancelListenConfig(p)
}

// SearchConfig  查询config
func (c *clientEntity) SearchConfig(p SearchParam) (*model.ConfigPage, error) {
	return c.client.SearchConfig(p)
}

// CloseClient 关闭grpc客户端
func (c *clientEntity) CloseClient() {
	c.client.CloseClient()
}
