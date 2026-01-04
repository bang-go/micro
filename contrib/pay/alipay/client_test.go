package alipay_test

import (
	"context"
	"strings"
	"testing"

	"github.com/bang-go/micro/contrib/pay/alipay"
	"github.com/go-pay/gopay"
)

func TestNew(t *testing.T) {
	cfg := &alipay.Config{
		AppId:      "your_app_id",
		PrivateKey: "your_private_key",
		IsProd:     false,
	}

	// 测试 Option (目前没有暴露额外的 Option，但机制是存在的)
	client, err := alipay.New(cfg)
	if err != nil {
		if strings.Contains(err.Error(), "pemContent decode error") {
			t.Log("skipping part of test due to invalid mock private key")
		} else {
			t.Fatalf("failed to new alipay client: %v", err)
		}
	}

	if client != nil {
		// 测试获取原始客户端
		if client.GetClient() == nil {
			t.Fatal("original client is nil")
		}
	}
}

func TestTradePagePay_Mock(t *testing.T) {
	// 这里只是演示用法，实际调用会报错（因为配置是假的）
	cfg := &alipay.Config{
		AppId:      "2016080100000001",
		PrivateKey: "MIIEowIBAAKCAQEA...", // 假的私钥
		IsProd:     false,
	}

	client, err := alipay.New(cfg)
	if err != nil {
		t.Logf("new client error: %v (expected with invalid key)", err)
		return
	}

	bm := make(gopay.BodyMap)
	bm.Set("subject", "测试订单")
	bm.Set("out_trade_no", "TRADE20230101001")
	bm.Set("total_amount", "0.01")
	bm.Set("product_code", "FAST_INSTANT_TRADE_PAY")

	// 预期会报错
	_, err = client.TradePagePay(context.Background(), bm)
	if err == nil {
		t.Log("unexpected success")
	} else {
		t.Logf("expected error: %v", err)
	}
}
