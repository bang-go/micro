package discovery

import (
	"errors"
	"testing"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
)

func TestPrepareConfigValidation(t *testing.T) {
	_, err := prepareConfig(nil)
	if !errors.Is(err, ErrNilConfig) {
		t.Fatalf("prepareConfig(nil) error = %v, want %v", err, ErrNilConfig)
	}

	_, err = prepareConfig(&Config{})
	if !errors.Is(err, ErrServerConfigMiss) {
		t.Fatalf("prepareConfig(empty) error = %v, want %v", err, ErrServerConfigMiss)
	}
}

func TestPrepareConfigUsesDefaultClientConfig(t *testing.T) {
	param, err := prepareConfig(&Config{
		ServerConfigs: []constant.ServerConfig{{IpAddr: "127.0.0.1", Port: 8848}},
	})
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if param.ClientConfig == nil {
		t.Fatal("ClientConfig = nil, want default config")
	}
}

func TestPrepareConfigAllowsEndpointWithoutServers(t *testing.T) {
	param, err := prepareConfig(&Config{
		ClientConfig: &constant.ClientConfig{Endpoint: " nacos.internal "},
	})
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if got, want := param.ClientConfig.Endpoint, "nacos.internal"; got != want {
		t.Fatalf("ClientConfig.Endpoint = %q, want %q", got, want)
	}
}

func TestPrepareConfigClonesInput(t *testing.T) {
	conf := &Config{
		ClientConfig: &constant.ClientConfig{
			Endpoint:      "nacos.internal",
			AppConnLabels: map[string]string{"env": "prod"},
		},
		ServerConfigs: []constant.ServerConfig{{IpAddr: "127.0.0.1", Port: 8848}},
	}

	param, err := prepareConfig(conf)
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}

	conf.ClientConfig.Endpoint = "mutated"
	conf.ClientConfig.AppConnLabels["env"] = "dev"
	conf.ServerConfigs[0].IpAddr = "192.168.1.1"

	if got, want := param.ClientConfig.Endpoint, "nacos.internal"; got != want {
		t.Fatalf("ClientConfig.Endpoint = %q, want %q", got, want)
	}
	if got, want := param.ClientConfig.AppConnLabels["env"], "prod"; got != want {
		t.Fatalf("ClientConfig.AppConnLabels[env] = %q, want %q", got, want)
	}
	if got, want := param.ServerConfigs[0].IpAddr, "127.0.0.1"; got != want {
		t.Fatalf("ServerConfigs[0].IpAddr = %q, want %q", got, want)
	}
}

func TestPrepareConfigIgnoresBlankAndDuplicateServers(t *testing.T) {
	param, err := prepareConfig(&Config{
		ServerConfigs: []constant.ServerConfig{
			{},
			{IpAddr: " 127.0.0.1 ", Port: 8848, Scheme: " HTTP ", ContextPath: " /nacos "},
			{IpAddr: "127.0.0.1", Port: 8848, Scheme: "http", ContextPath: "/nacos"},
		},
	})
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}

	if got, want := len(param.ServerConfigs), 1; got != want {
		t.Fatalf("len(ServerConfigs) = %d, want %d", got, want)
	}
	if got, want := param.ServerConfigs[0].IpAddr, "127.0.0.1"; got != want {
		t.Fatalf("ServerConfigs[0].IpAddr = %q, want %q", got, want)
	}
	if got, want := param.ServerConfigs[0].Scheme, "http"; got != want {
		t.Fatalf("ServerConfigs[0].Scheme = %q, want %q", got, want)
	}
	if got, want := param.ServerConfigs[0].ContextPath, "/nacos"; got != want {
		t.Fatalf("ServerConfigs[0].ContextPath = %q, want %q", got, want)
	}
}

func TestPrepareConfigBlankServerEntriesDoNotSatisfyValidation(t *testing.T) {
	_, err := prepareConfig(&Config{
		ServerConfigs: []constant.ServerConfig{{}},
	})
	if !errors.Is(err, ErrServerConfigMiss) {
		t.Fatalf("prepareConfig(blank server configs) error = %v, want %v", err, ErrServerConfigMiss)
	}
}

func TestNew(t *testing.T) {
	client, err := New(&Config{
		ClientConfig: &constant.ClientConfig{
			NamespaceId: "public",
			CacheDir:    t.TempDir(),
			LogDir:      t.TempDir(),
		},
		ServerConfigs: []constant.ServerConfig{{IpAddr: "127.0.0.1", Port: 8848}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client == nil {
		t.Fatal("New() client = nil")
	}
}
