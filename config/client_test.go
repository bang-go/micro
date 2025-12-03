package config_test

import (
	"log"
	"testing"

	"github.com/bang-go/micro/config"
)

func TestClientConfig(t *testing.T) {
	var err error
	client, err := config.New(&config.ClientConfig{
		NamespaceId:         "", //we can create multiple clients with different namespaceId to support multiple namespace.When namespace is public, fill in the blank string here.
		TimeoutMs:           100000,
		NotLoadCacheAtStart: true,
		LogDir:              "./tmp/nacos/log",
		CacheDir:            "./tmp/nacos/cache",
		LogLevel:            "error",
		AppendToStdout:      true,
	}, []config.ServerConfig{{
		IpAddr: "",
		Port:   8848,
	}})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(client)
	_, err = client.PublishConfig(config.Param{
		DataId:  "test",
		Group:   "test",
		Content: "hello world!",
	})
	//str, err := client.GetConfig(config.Param{
	//	DataId: "cs2.game.trigger.create_server",
	//	Group:  "game",
	//})
	//
	//if err != nil {
	//	log.Fatal(err)
	//	os.Exit(1)
	//}
	//log.Println("success", str)
}
