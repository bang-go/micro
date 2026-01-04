package wechat_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/bang-go/micro/contrib/pay/wechat"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
)

func TestNew(t *testing.T) {
	// 构造配置
	cfg := &wechat.Config{
		AppId:                      "wx_app_id",
		MchId:                      "1234567890",
		MchCertificateSerialNumber: "SERIAL_NUMBER",
		MchAPIv3Key:                "api_v3_key",
		MchPrivateKeyPath:          "testdata/apiclient_key.pem", // 假设有一个测试用的私钥文件
		NotifyUrl:                  "https://example.com/notify",
	}

	// 1. 测试参数校验
	_, err := wechat.New(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when config is nil")
	}

	// 2. 测试 Option 和私钥加载失败
	// 即使私钥加载失败，Option 的逻辑也应该被执行（虽然这里无法验证 Option 的副作用，除非我们 Mock LoadPrivateKey）
	// 但我们可以验证传递 Option 不会 panic
	_, err = wechat.New(context.Background(), cfg, wechat.WithHttpClient(http.DefaultClient))
	if err == nil {
		t.Fatal("expected error when private key file does not exist")
	} else {
		t.Logf("expected error: %v", err)
	}
}

func TestJsapiPrepay_Mock(t *testing.T) {
	t.Skip("Skipping execution because valid private key file is required for wechatpay-go")

	cfg := &wechat.Config{
		AppId:                      "wx_app_id",
		MchId:                      "1234567890",
		MchCertificateSerialNumber: "SERIAL_NUMBER",
		MchAPIv3Key:                "api_v3_key",
		MchPrivateKeyPath:          "/path/to/real/key.pem",
		NotifyUrl:                  "https://example.com/notify",
	}

	client, err := wechat.New(context.Background(), cfg)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			t.Skip("skipping test due to missing private key file")
		}
		t.Fatalf("failed to new client: %v", err)
	}

	req := jsapi.PrepayRequest{
		Description: core.String("测试订单"),
		OutTradeNo:  core.String("TRADE20230101001"),
		Amount: &jsapi.Amount{
			Total: core.Int64(1),
		},
		Payer: &jsapi.Payer{
			Openid: core.String("oUpF8uMuAJO_M2pxb1Q9zNjWeS6o"),
		},
	}

	_, err = client.JsapiPrepay(context.Background(), req)
	if err == nil {
		t.Log("unexpected success")
	} else {
		t.Logf("expected error: %v", err)
	}
}
