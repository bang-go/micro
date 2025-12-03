package search_test

import (
	"fmt"
	"testing"

	teaUtil "github.com/alibabacloud-go/tea-utils/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/bang-go/micro/search"
	"github.com/bang-go/util"
)

func TestClient(t *testing.T) {
	var err error
	client, err := search.NewClient(&search.Config{
		Endpoint:        util.VarAddr(""),
		AccessKeyId:     util.VarAddr(""),
		AccessKeySecret: util.VarAddr("")})
	if err != nil {
		panic(err)
	}
	// 请求发送的配置参数. 用以请求配置及连接池配置.
	runtime := &teaUtil.RuntimeOptions{
		ConnectTimeout: tea.Int(5000),
		ReadTimeout:    tea.Int(10000),
		Autoretry:      tea.Bool(false),
		IgnoreSSL:      tea.Bool(false),
		MaxIdleConns:   tea.Int(50),
	}
	requestParams := map[string]interface{}{
		"hit":   10,
		"query": "1",
	}
	appName := ""
	modelName := ""
	response, _requestErr := client.Request(
		tea.String("GET"),
		tea.String("/v3/openapi/apps/"+appName+"/suggest/"+modelName+"/search"),
		requestParams,
		nil,
		nil,
		runtime)
	// 如果 发送请求 过程中出现异常. 则 返回 _requestErr 且输出 错误信息.
	if _requestErr != nil {
		fmt.Println(_requestErr)
		panic(1)
	}

	// 输出正常返回的 response 内容.
	fmt.Println(response)
}
