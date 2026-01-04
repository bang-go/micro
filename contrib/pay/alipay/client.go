package alipay

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-pay/gopay"
	"github.com/go-pay/gopay/alipay"
)

// Config 支付宝配置
type Config struct {
	AppId          string `json:"app_id"`           // 应用ID
	PrivateKey     string `json:"private_key"`      // 应用私钥
	IsProd         bool   `json:"is_prod"`          // 是否正式环境
	AppCertPath    string `json:"app_cert_path"`    // 应用公钥证书路径
	RootCertPath   string `json:"root_cert_path"`   // 支付宝根证书路径
	PublicCertPath string `json:"public_cert_path"` // 支付宝公钥证书路径
	NotifyUrl      string `json:"notify_url"`       // 异步通知地址
	ReturnUrl      string `json:"return_url"`       // 同步跳转地址
	Charset        string `json:"charset"`          // 编码格式，默认 utf-8
	SignType       string `json:"sign_type"`        // 签名算法，默认 RSA2
}

// Option 定义可选配置
type Option func(*client)

// Client 支付宝客户端接口
type Client interface {
	// TradePagePay 电脑网站支付
	TradePagePay(ctx context.Context, bm gopay.BodyMap) (string, error)
	// TradeWapPay 手机网站支付
	TradeWapPay(ctx context.Context, bm gopay.BodyMap) (string, error)
	// TradeAppPay APP支付
	TradeAppPay(ctx context.Context, bm gopay.BodyMap) (string, error)
	// TradePrecreate 扫码支付
	TradePrecreate(ctx context.Context, bm gopay.BodyMap) (*alipay.TradePrecreateResponse, error)
	// TradePay 付款码支付
	TradePay(ctx context.Context, bm gopay.BodyMap) (*alipay.TradePayResponse, error)
	// TradeQuery 统一收单线下交易查询
	TradeQuery(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeQueryResponse, error)
	// TradeClose 统一收单交易关闭接口
	TradeClose(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeCloseResponse, error)
	// TradeRefund 统一收单交易退款接口
	TradeRefund(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeRefundResponse, error)
	// TradeRefundQuery 统一收单交易退款查询
	TradeRefundQuery(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeFastpayRefundQueryResponse, error)
	// TradeBillDownloadQuery 查询对账单下载地址
	TradeBillDownloadQuery(ctx context.Context, bm gopay.BodyMap) (string, error)

	// ParseNotify 解析并验证回调通知
	ParseNotify(req *http.Request) (gopay.BodyMap, error)

	// GetClient 获取原始客户端
	GetClient() *alipay.Client
}

type client struct {
	cli *alipay.Client
	cfg *Config
}

// New 创建支付宝客户端
func New(cfg *Config, opts ...Option) (Client, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}
	if cfg.AppId == "" || cfg.PrivateKey == "" {
		return nil, errors.New("appid or private key is empty")
	}

	c := &client{
		cfg: cfg,
	}
	for _, opt := range opts {
		opt(c)
	}

	// 初始化支付宝客户端
	cli, err := alipay.NewClient(cfg.AppId, cfg.PrivateKey, cfg.IsProd)
	if err != nil {
		return nil, fmt.Errorf("failed to new alipay client: %v", err)
	}

	// 配置公共参数
	cli.SetCharset(cfg.Charset)
	cli.SetSignType(cfg.SignType)
	if cfg.NotifyUrl != "" {
		cli.SetNotifyUrl(cfg.NotifyUrl)
	}
	if cfg.ReturnUrl != "" {
		cli.SetReturnUrl(cfg.ReturnUrl)
	}

	// 配置证书 (推荐)
	if cfg.AppCertPath != "" && cfg.RootCertPath != "" && cfg.PublicCertPath != "" {
		err = cli.SetCertSnByPath(cfg.AppCertPath, cfg.RootCertPath, cfg.PublicCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to set cert sn: %v", err)
		}
	}

	// 开启自动验签（如果配置了公钥证书，gopay 会自动处理）
	// 注意：gopay 的 AutoVerifySign 需要传入支付宝公钥内容。
	// 如果使用了 SetCertSnByPath，gopay 在处理回调时可能需要我们显式调用 VerifySignWithCert

	c.cli = cli
	return c, nil
}

func (c *client) TradePagePay(ctx context.Context, bm gopay.BodyMap) (string, error) {
	return c.cli.TradePagePay(ctx, bm)
}

func (c *client) TradeWapPay(ctx context.Context, bm gopay.BodyMap) (string, error) {
	return c.cli.TradeWapPay(ctx, bm)
}

func (c *client) TradeAppPay(ctx context.Context, bm gopay.BodyMap) (string, error) {
	return c.cli.TradeAppPay(ctx, bm)
}

func (c *client) TradePrecreate(ctx context.Context, bm gopay.BodyMap) (*alipay.TradePrecreateResponse, error) {
	return c.cli.TradePrecreate(ctx, bm)
}

func (c *client) TradePay(ctx context.Context, bm gopay.BodyMap) (*alipay.TradePayResponse, error) {
	return c.cli.TradePay(ctx, bm)
}

func (c *client) TradeQuery(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeQueryResponse, error) {
	return c.cli.TradeQuery(ctx, bm)
}

func (c *client) TradeClose(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeCloseResponse, error) {
	return c.cli.TradeClose(ctx, bm)
}

func (c *client) TradeRefund(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeRefundResponse, error) {
	return c.cli.TradeRefund(ctx, bm)
}

func (c *client) TradeRefundQuery(ctx context.Context, bm gopay.BodyMap) (*alipay.TradeFastpayRefundQueryResponse, error) {
	return c.cli.TradeFastPayRefundQuery(ctx, bm)
}

func (c *client) TradeBillDownloadQuery(ctx context.Context, bm gopay.BodyMap) (string, error) {
	rsp, err := c.cli.DataBillDownloadUrlQuery(ctx, bm)
	if err != nil {
		return "", err
	}
	if rsp.Response.BillDownloadUrl == "" {
		return "", fmt.Errorf("bill download url is empty, response: %+v", rsp)
	}
	return rsp.Response.BillDownloadUrl, nil
}

func (c *client) ParseNotify(req *http.Request) (gopay.BodyMap, error) {
	// 解析回调参数
	notifyReq, err := alipay.ParseNotifyToBodyMap(req)
	if err != nil {
		return nil, err
	}

	// 验签
	var ok bool
	if c.cfg.PublicCertPath != "" {
		// 证书模式验签
		ok, err = alipay.VerifySignWithCert(c.cfg.PublicCertPath, notifyReq)
	} else {
		// 普通公钥验签 (注意：这里需要用户配置支付宝公钥，不仅仅是证书路径)
		// 我们的 Config 里目前只有 PublicCertPath。
		// 如果用户用的是公钥模式，应该加一个 AlipayPublicKey 字段。
		// 为了简化，假设企业级用户都用证书模式。
		// 如果没有配置 PublicCertPath，则无法验签（或者需要补充字段）。
		return nil, errors.New("missing public cert path for verify sign")
	}

	if err != nil {
		return nil, fmt.Errorf("verify sign error: %v", err)
	}
	if !ok {
		return nil, errors.New("verify sign failed")
	}

	return notifyReq, nil
}

func (c *client) GetClient() *alipay.Client {
	return c.cli
}
