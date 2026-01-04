package wechat

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/app"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/h5"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

// Config 微信支付配置
type Config struct {
	AppId                      string `json:"app_id"`                        // 应用ID
	MchId                      string `json:"mch_id"`                        // 商户号
	MchCertificateSerialNumber string `json:"mch_certificate_serial_number"` // 商户证书序列号
	MchAPIv3Key                string `json:"mch_api_v3_key"`                // 商户APIv3密钥
	MchPrivateKeyPath          string `json:"mch_private_key_path"`          // 商户私钥路径
	NotifyUrl                  string `json:"notify_url"`                    // 默认通知地址
}

// Option 定义可选配置
type Option func(*client)

// WithHttpClient 设置自定义 HTTP Client (可用于链路追踪)
func WithHttpClient(cli *http.Client) Option {
	return func(c *client) {
		c.httpClient = cli
	}
}

// Client 微信支付客户端接口
type Client interface {
	// JsapiPrepay JSAPI/小程序下单
	JsapiPrepay(ctx context.Context, req jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error)
	// NativePrepay Native扫码下单
	NativePrepay(ctx context.Context, req native.PrepayRequest) (*native.PrepayResponse, error)
	// AppPrepay APP下单
	AppPrepay(ctx context.Context, req app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error)
	// H5Prepay H5下单
	H5Prepay(ctx context.Context, req h5.PrepayRequest) (*h5.PrepayResponse, error)

	// QueryOrderByOutTradeNo 查询订单 (通过商户订单号)
	QueryOrderByOutTradeNo(ctx context.Context, outTradeNo string) (*payments.Transaction, error)
	// CloseOrder 关闭订单
	CloseOrder(ctx context.Context, outTradeNo string) error

	// Refund 申请退款
	Refund(ctx context.Context, req refunddomestic.CreateRequest) (*refunddomestic.Refund, error)
	// QueryRefund 查询退款
	QueryRefund(ctx context.Context, outRefundNo string) (*refunddomestic.Refund, error)

	// ParseNotify 解析回调通知
	ParseNotify(req *http.Request, content interface{}) (*notify.Request, error)

	// GetClient 获取原始客户端
	GetClient() *core.Client
}

type client struct {
	cli        *core.Client
	cfg        *Config
	handler    *notify.Handler
	httpClient *http.Client
}

// New 创建微信支付客户端
func New(ctx context.Context, cfg *Config, opts ...Option) (Client, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}
	if cfg.AppId == "" || cfg.MchId == "" || cfg.MchCertificateSerialNumber == "" || cfg.MchAPIv3Key == "" || cfg.MchPrivateKeyPath == "" {
		return nil, errors.New("missing required config fields")
	}

	c := &client{
		cfg: cfg,
	}
	for _, opt := range opts {
		opt(c)
	}

	// 加载商户私钥
	mchPrivateKey, err := utils.LoadPrivateKeyWithPath(cfg.MchPrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load merchant private key error: %w", err)
	}

	// 核心配置
	coreOpts := []core.ClientOption{
		option.WithWechatPayAutoAuthCipher(cfg.MchId, cfg.MchCertificateSerialNumber, mchPrivateKey, cfg.MchAPIv3Key),
	}

	// 注入自定义 HTTP Client
	if c.httpClient != nil {
		coreOpts = append(coreOpts, option.WithHTTPClient(c.httpClient))
	}

	cli, err := core.NewClient(ctx, coreOpts...)
	if err != nil {
		return nil, fmt.Errorf("new wechat pay client error: %w", err)
	}
	c.cli = cli

	// 初始化回调通知处理器
	mgr := downloader.MgrInstance()
	// 注册下载器（使用相同的 HTTP Client）
	// 注意：RegisterDownloaderWithClient 会直接使用 client 内部的 http client，所以不需要重复注入
	err = mgr.RegisterDownloaderWithClient(ctx, cli, cfg.MchId, cfg.MchAPIv3Key)
	if err != nil {
		// 记录错误但不中断
	}

	certVisitor := mgr.GetCertificateVisitor(cfg.MchId)
	verifier := verifiers.NewSHA256WithRSAVerifier(certVisitor)
	handler, err := notify.NewRSANotifyHandler(cfg.MchAPIv3Key, verifier)
	if err != nil {
		return nil, fmt.Errorf("new notify handler err: %v", err)
	}
	c.handler = handler

	return c, nil
}

func (c *client) JsapiPrepay(ctx context.Context, req jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error) {
	if req.Appid == nil {
		req.Appid = core.String(c.cfg.AppId)
	}
	if req.Mchid == nil {
		req.Mchid = core.String(c.cfg.MchId)
	}
	if req.NotifyUrl == nil && c.cfg.NotifyUrl != "" {
		req.NotifyUrl = core.String(c.cfg.NotifyUrl)
	}

	svc := jsapi.JsapiApiService{Client: c.cli}
	rsp, _, err := svc.PrepayWithRequestPayment(ctx, req)
	return rsp, err
}

func (c *client) NativePrepay(ctx context.Context, req native.PrepayRequest) (*native.PrepayResponse, error) {
	if req.Appid == nil {
		req.Appid = core.String(c.cfg.AppId)
	}
	if req.Mchid == nil {
		req.Mchid = core.String(c.cfg.MchId)
	}
	if req.NotifyUrl == nil && c.cfg.NotifyUrl != "" {
		req.NotifyUrl = core.String(c.cfg.NotifyUrl)
	}

	svc := native.NativeApiService{Client: c.cli}
	rsp, _, err := svc.Prepay(ctx, req)
	return rsp, err
}

func (c *client) AppPrepay(ctx context.Context, req app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error) {
	if req.Appid == nil {
		req.Appid = core.String(c.cfg.AppId)
	}
	if req.Mchid == nil {
		req.Mchid = core.String(c.cfg.MchId)
	}
	if req.NotifyUrl == nil && c.cfg.NotifyUrl != "" {
		req.NotifyUrl = core.String(c.cfg.NotifyUrl)
	}

	svc := app.AppApiService{Client: c.cli}
	rsp, _, err := svc.PrepayWithRequestPayment(ctx, req)
	return rsp, err
}

func (c *client) H5Prepay(ctx context.Context, req h5.PrepayRequest) (*h5.PrepayResponse, error) {
	if req.Appid == nil {
		req.Appid = core.String(c.cfg.AppId)
	}
	if req.Mchid == nil {
		req.Mchid = core.String(c.cfg.MchId)
	}
	if req.NotifyUrl == nil && c.cfg.NotifyUrl != "" {
		req.NotifyUrl = core.String(c.cfg.NotifyUrl)
	}

	svc := h5.H5ApiService{Client: c.cli}
	rsp, _, err := svc.Prepay(ctx, req)
	return rsp, err
}

func (c *client) QueryOrderByOutTradeNo(ctx context.Context, outTradeNo string) (*payments.Transaction, error) {
	svc := jsapi.JsapiApiService{Client: c.cli}
	req := jsapi.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(outTradeNo),
		Mchid:      core.String(c.cfg.MchId),
	}
	rsp, _, err := svc.QueryOrderByOutTradeNo(ctx, req)
	return rsp, err
}

func (c *client) CloseOrder(ctx context.Context, outTradeNo string) error {
	svc := jsapi.JsapiApiService{Client: c.cli}
	req := jsapi.CloseOrderRequest{
		OutTradeNo: core.String(outTradeNo),
		Mchid:      core.String(c.cfg.MchId),
	}
	_, err := svc.CloseOrder(ctx, req)
	return err
}

func (c *client) Refund(ctx context.Context, req refunddomestic.CreateRequest) (*refunddomestic.Refund, error) {
	svc := refunddomestic.RefundsApiService{Client: c.cli}
	rsp, _, err := svc.Create(ctx, req)
	return rsp, err
}

func (c *client) QueryRefund(ctx context.Context, outRefundNo string) (*refunddomestic.Refund, error) {
	svc := refunddomestic.RefundsApiService{Client: c.cli}
	req := refunddomestic.QueryByOutRefundNoRequest{
		OutRefundNo: core.String(outRefundNo),
	}
	rsp, _, err := svc.QueryByOutRefundNo(ctx, req)
	return rsp, err
}

func (c *client) ParseNotify(req *http.Request, content interface{}) (*notify.Request, error) {
	if c.handler == nil {
		return nil, errors.New("notify handler is not initialized")
	}
	return c.handler.ParseNotifyRequest(context.Background(), req, content)
}

func (c *client) GetClient() *core.Client {
	return c.cli
}
