package discovery

import (
	"strconv"
	"strings"

	"github.com/bang-go/util"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/common/security"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type Config struct {
	ClientConfig          *constant.ClientConfig
	ServerConfigs         []constant.ServerConfig
	RamCredentialProvider security.RamCredentialProvider
}

func Open(conf *Config) (naming_client.INamingClient, error) {
	param, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}
	return clients.NewNamingClient(param)
}

func New(conf *Config) (naming_client.INamingClient, error) {
	return Open(conf)
}

func prepareConfig(conf *Config) (vo.NacosClientParam, error) {
	if conf == nil {
		return vo.NacosClientParam{}, ErrNilConfig
	}

	clientConfig := cloneClientConfig(conf.ClientConfig)
	if clientConfig == nil {
		clientConfig = constant.NewClientConfig()
	}

	serverConfigs := cloneServerConfigs(conf.ServerConfigs)
	if len(serverConfigs) == 0 && strings.TrimSpace(clientConfig.Endpoint) == "" {
		return vo.NacosClientParam{}, ErrServerConfigMiss
	}

	return vo.NacosClientParam{
		ClientConfig:          clientConfig,
		ServerConfigs:         serverConfigs,
		RamCredentialProvider: conf.RamCredentialProvider,
	}, nil
}

func cloneClientConfig(src *constant.ClientConfig) *constant.ClientConfig {
	if src == nil {
		return nil
	}

	cloned := *src
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.RamConfig = util.ClonePtr(src.RamConfig)
	cloned.KMSv3Config = util.ClonePtr(src.KMSv3Config)
	cloned.KMSConfig = util.ClonePtr(src.KMSConfig)
	cloned.LogSampling = util.ClonePtr(src.LogSampling)
	cloned.LogRollingConfig = util.ClonePtr(src.LogRollingConfig)
	if src.AppConnLabels != nil {
		cloned.AppConnLabels = make(map[string]string, len(src.AppConnLabels))
		for key, value := range src.AppConnLabels {
			cloned.AppConnLabels[key] = value
		}
	}
	return &cloned
}

func cloneServerConfigs(src []constant.ServerConfig) []constant.ServerConfig {
	if len(src) == 0 {
		return nil
	}

	cloned := make([]constant.ServerConfig, 0, len(src))
	seen := make(map[string]struct{}, len(src))
	for _, serverConfig := range src {
		serverConfig.Scheme = strings.ToLower(strings.TrimSpace(serverConfig.Scheme))
		serverConfig.ContextPath = strings.TrimSpace(serverConfig.ContextPath)
		serverConfig.IpAddr = strings.TrimSpace(serverConfig.IpAddr)

		if isBlankServerConfig(serverConfig) {
			continue
		}

		key := serverConfig.Scheme + "\x00" + serverConfig.ContextPath + "\x00" + serverConfig.IpAddr + "\x00" +
			strconv.FormatUint(serverConfig.Port, 10) + "\x00" + strconv.FormatUint(serverConfig.GrpcPort, 10)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cloned = append(cloned, serverConfig)
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func isBlankServerConfig(conf constant.ServerConfig) bool {
	return conf.Scheme == "" &&
		conf.ContextPath == "" &&
		conf.IpAddr == "" &&
		conf.Port == 0 &&
		conf.GrpcPort == 0
}
