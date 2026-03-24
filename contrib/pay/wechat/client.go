package wechat

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	coreoption "github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/app"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/h5"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

var (
	ErrNilConfig                  = errors.New("wechat: config is required")
	ErrContextRequired            = errors.New("wechat: context is required")
	ErrAppIDRequired              = errors.New("wechat: app id is required")
	ErrMchIDRequired              = errors.New("wechat: merchant id is required")
	ErrCertificateSerialRequired  = errors.New("wechat: merchant certificate serial number is required")
	ErrAPIv3KeyRequired           = errors.New("wechat: merchant api v3 key is required")
	ErrPrivateKeyPathRequired     = errors.New("wechat: merchant private key path is required")
	ErrOutTradeNoRequired         = errors.New("wechat: out trade no is required")
	ErrOutRefundNoRequired        = errors.New("wechat: out refund no is required")
	ErrNotifyRequestRequired      = errors.New("wechat: notify request is required")
	ErrNotifyHandlerUninitialized = errors.New("wechat: notify handler is not initialized")
)

type Config struct {
	AppID                      string `json:"app_id"`
	MchID                      string `json:"mch_id"`
	MchCertificateSerialNumber string `json:"mch_certificate_serial_number"`
	MchAPIv3Key                string `json:"mch_api_v3_key"`
	MchPrivateKeyPath          string `json:"mch_private_key_path"`
	NotifyURL                  string `json:"notify_url"`

	loadPrivateKey   func(string) (*rsa.PrivateKey, error)
	newClient        func(context.Context, ...core.ClientOption) (*core.Client, error)
	downloader       certificateRegistry
	newNotifyHandler func(string, auth.Verifier) (notifyParser, error)
	newPayments      func(*core.Client) paymentAPI
	newRefunds       func(*core.Client) refundAPI
}

type Option func(*options)

type options struct {
	httpClient *http.Client
}

func WithHTTPClient(cli *http.Client) Option {
	return func(o *options) {
		o.httpClient = cli
	}
}

func WithHttpClient(cli *http.Client) Option {
	return WithHTTPClient(cli)
}

type Client interface {
	JsapiPrepay(context.Context, jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error)
	NativePrepay(context.Context, native.PrepayRequest) (*native.PrepayResponse, error)
	AppPrepay(context.Context, app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error)
	H5Prepay(context.Context, h5.PrepayRequest) (*h5.PrepayResponse, error)

	QueryOrderByOutTradeNo(context.Context, string) (*payments.Transaction, error)
	CloseOrder(context.Context, string) error

	Refund(context.Context, refunddomestic.CreateRequest) (*refunddomestic.Refund, error)
	QueryRefund(context.Context, string) (*refunddomestic.Refund, error)

	ParseNotify(*http.Request, any) (*notify.Request, error)

	Raw() *core.Client
	GetClient() *core.Client
}

type paymentAPI interface {
	JsapiPrepay(context.Context, jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error)
	NativePrepay(context.Context, native.PrepayRequest) (*native.PrepayResponse, error)
	AppPrepay(context.Context, app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error)
	H5Prepay(context.Context, h5.PrepayRequest) (*h5.PrepayResponse, error)
	QueryOrderByOutTradeNo(context.Context, jsapi.QueryOrderByOutTradeNoRequest) (*payments.Transaction, error)
	CloseOrder(context.Context, jsapi.CloseOrderRequest) error
}

type refundAPI interface {
	Refund(context.Context, refunddomestic.CreateRequest) (*refunddomestic.Refund, error)
	QueryRefund(context.Context, refunddomestic.QueryByOutRefundNoRequest) (*refunddomestic.Refund, error)
}

type notifyParser interface {
	ParseNotifyRequest(context.Context, *http.Request, any) (*notify.Request, error)
}

type certificateRegistry interface {
	RegisterDownloaderWithClient(context.Context, *core.Client, string, string) error
	GetCertificateVisitor(string) core.CertificateVisitor
}

type client struct {
	raw      *core.Client
	config   *Config
	payments paymentAPI
	refunds  refundAPI
	handler  notifyParser
}

func Open(ctx context.Context, cfg *Config, opts ...Option) (Client, error) {
	return New(ctx, cfg, opts...)
}

func New(ctx context.Context, cfg *Config, opts ...Option) (Client, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	config, err := prepareConfig(cfg)
	if err != nil {
		return nil, err
	}

	settings := options{}
	for _, opt := range opts {
		opt(&settings)
	}

	privateKey, err := config.loadPrivateKey(config.MchPrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("wechat: load merchant private key failed: %w", err)
	}

	clientOptions := []core.ClientOption{
		coreoption.WithWechatPayAutoAuthCipher(
			config.MchID,
			config.MchCertificateSerialNumber,
			privateKey,
			config.MchAPIv3Key,
		),
	}
	if settings.httpClient != nil {
		clientOptions = append(clientOptions, coreoption.WithHTTPClient(settings.httpClient))
	}

	raw, err := config.newClient(ctx, clientOptions...)
	if err != nil {
		return nil, fmt.Errorf("wechat: create client failed: %w", err)
	}

	if err := config.downloader.RegisterDownloaderWithClient(ctx, raw, config.MchID, config.MchAPIv3Key); err != nil {
		return nil, fmt.Errorf("wechat: register certificate downloader failed: %w", err)
	}

	handler, err := config.newNotifyHandler(
		config.MchAPIv3Key,
		verifiers.NewSHA256WithRSAVerifier(config.downloader.GetCertificateVisitor(config.MchID)),
	)
	if err != nil {
		return nil, fmt.Errorf("wechat: create notify handler failed: %w", err)
	}

	return &client{
		raw:      raw,
		config:   config,
		payments: config.newPayments(raw),
		refunds:  config.newRefunds(raw),
		handler:  handler,
	}, nil
}

func (c *client) JsapiPrepay(ctx context.Context, req jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	applyPrepayDefaults(&req.Appid, &req.Mchid, &req.NotifyUrl, c.config)
	return c.payments.JsapiPrepay(ctx, req)
}

func (c *client) NativePrepay(ctx context.Context, req native.PrepayRequest) (*native.PrepayResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	applyPrepayDefaults(&req.Appid, &req.Mchid, &req.NotifyUrl, c.config)
	return c.payments.NativePrepay(ctx, req)
}

func (c *client) AppPrepay(ctx context.Context, req app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	applyPrepayDefaults(&req.Appid, &req.Mchid, &req.NotifyUrl, c.config)
	return c.payments.AppPrepay(ctx, req)
}

func (c *client) H5Prepay(ctx context.Context, req h5.PrepayRequest) (*h5.PrepayResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	applyPrepayDefaults(&req.Appid, &req.Mchid, &req.NotifyUrl, c.config)
	return c.payments.H5Prepay(ctx, req)
}

func (c *client) QueryOrderByOutTradeNo(ctx context.Context, outTradeNo string) (*payments.Transaction, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return nil, ErrOutTradeNoRequired
	}

	return c.payments.QueryOrderByOutTradeNo(ctx, jsapi.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(outTradeNo),
		Mchid:      core.String(c.config.MchID),
	})
}

func (c *client) CloseOrder(ctx context.Context, outTradeNo string) error {
	if ctx == nil {
		return ErrContextRequired
	}
	outTradeNo = strings.TrimSpace(outTradeNo)
	if outTradeNo == "" {
		return ErrOutTradeNoRequired
	}

	return c.payments.CloseOrder(ctx, jsapi.CloseOrderRequest{
		OutTradeNo: core.String(outTradeNo),
		Mchid:      core.String(c.config.MchID),
	})
}

func (c *client) Refund(ctx context.Context, req refunddomestic.CreateRequest) (*refunddomestic.Refund, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	return c.refunds.Refund(ctx, req)
}

func (c *client) QueryRefund(ctx context.Context, outRefundNo string) (*refunddomestic.Refund, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	outRefundNo = strings.TrimSpace(outRefundNo)
	if outRefundNo == "" {
		return nil, ErrOutRefundNoRequired
	}

	return c.refunds.QueryRefund(ctx, refunddomestic.QueryByOutRefundNoRequest{
		OutRefundNo: core.String(outRefundNo),
	})
}

func (c *client) ParseNotify(req *http.Request, content any) (*notify.Request, error) {
	if req == nil {
		return nil, ErrNotifyRequestRequired
	}
	if c.handler == nil {
		return nil, ErrNotifyHandlerUninitialized
	}
	return c.handler.ParseNotifyRequest(req.Context(), req, content)
}

func (c *client) Raw() *core.Client {
	return c.raw
}

func (c *client) GetClient() *core.Client {
	return c.Raw()
}

func applyPrepayDefaults(appID, mchID, notifyURL **string, cfg *Config) {
	applyStringDefault(appID, cfg.AppID)
	applyStringDefault(mchID, cfg.MchID)
	applyStringDefault(notifyURL, cfg.NotifyURL)
}

func prepareConfig(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	cloned := *cfg
	cloned.AppID = strings.TrimSpace(cloned.AppID)
	cloned.MchID = strings.TrimSpace(cloned.MchID)
	cloned.MchCertificateSerialNumber = strings.TrimSpace(cloned.MchCertificateSerialNumber)
	cloned.MchAPIv3Key = strings.TrimSpace(cloned.MchAPIv3Key)
	cloned.MchPrivateKeyPath = strings.TrimSpace(cloned.MchPrivateKeyPath)
	cloned.NotifyURL = strings.TrimSpace(cloned.NotifyURL)

	switch {
	case cloned.AppID == "":
		return nil, ErrAppIDRequired
	case cloned.MchID == "":
		return nil, ErrMchIDRequired
	case cloned.MchCertificateSerialNumber == "":
		return nil, ErrCertificateSerialRequired
	case cloned.MchAPIv3Key == "":
		return nil, ErrAPIv3KeyRequired
	case cloned.MchPrivateKeyPath == "":
		return nil, ErrPrivateKeyPathRequired
	}

	if cloned.loadPrivateKey == nil {
		cloned.loadPrivateKey = utils.LoadPrivateKeyWithPath
	}
	if cloned.newClient == nil {
		cloned.newClient = core.NewClient
	}
	if cloned.downloader == nil {
		cloned.downloader = certificateManagerAdapter{mgr: downloader.MgrInstance()}
	}
	if cloned.newNotifyHandler == nil {
		cloned.newNotifyHandler = func(apiV3Key string, verifier auth.Verifier) (notifyParser, error) {
			return notify.NewRSANotifyHandler(apiV3Key, verifier)
		}
	}
	if cloned.newPayments == nil {
		cloned.newPayments = func(raw *core.Client) paymentAPI {
			return sdkPaymentAPI{raw: raw}
		}
	}
	if cloned.newRefunds == nil {
		cloned.newRefunds = func(raw *core.Client) refundAPI {
			return sdkRefundAPI{raw: raw}
		}
	}

	return &cloned, nil
}

func applyStringDefault(target **string, fallback string) {
	if fallback == "" && *target == nil {
		return
	}
	if *target == nil {
		*target = core.String(fallback)
		return
	}

	value := strings.TrimSpace(**target)
	if value == "" {
		if fallback == "" {
			*target = nil
			return
		}
		*target = core.String(fallback)
		return
	}
	if value != **target {
		*target = core.String(value)
	}
}

type certificateManagerAdapter struct {
	mgr *downloader.CertificateDownloaderMgr
}

func (a certificateManagerAdapter) RegisterDownloaderWithClient(ctx context.Context, raw *core.Client, mchID, apiV3Key string) error {
	return a.mgr.RegisterDownloaderWithClient(ctx, raw, mchID, apiV3Key)
}

func (a certificateManagerAdapter) GetCertificateVisitor(mchID string) core.CertificateVisitor {
	return a.mgr.GetCertificateVisitor(mchID)
}

type sdkPaymentAPI struct {
	raw *core.Client
}

func (s sdkPaymentAPI) JsapiPrepay(ctx context.Context, req jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error) {
	service := jsapi.JsapiApiService{Client: s.raw}
	response, _, err := service.PrepayWithRequestPayment(ctx, req)
	return response, err
}

func (s sdkPaymentAPI) NativePrepay(ctx context.Context, req native.PrepayRequest) (*native.PrepayResponse, error) {
	service := native.NativeApiService{Client: s.raw}
	response, _, err := service.Prepay(ctx, req)
	return response, err
}

func (s sdkPaymentAPI) AppPrepay(ctx context.Context, req app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error) {
	service := app.AppApiService{Client: s.raw}
	response, _, err := service.PrepayWithRequestPayment(ctx, req)
	return response, err
}

func (s sdkPaymentAPI) H5Prepay(ctx context.Context, req h5.PrepayRequest) (*h5.PrepayResponse, error) {
	service := h5.H5ApiService{Client: s.raw}
	response, _, err := service.Prepay(ctx, req)
	return response, err
}

func (s sdkPaymentAPI) QueryOrderByOutTradeNo(ctx context.Context, req jsapi.QueryOrderByOutTradeNoRequest) (*payments.Transaction, error) {
	service := jsapi.JsapiApiService{Client: s.raw}
	response, _, err := service.QueryOrderByOutTradeNo(ctx, req)
	return response, err
}

func (s sdkPaymentAPI) CloseOrder(ctx context.Context, req jsapi.CloseOrderRequest) error {
	service := jsapi.JsapiApiService{Client: s.raw}
	_, err := service.CloseOrder(ctx, req)
	return err
}

type sdkRefundAPI struct {
	raw *core.Client
}

func (s sdkRefundAPI) Refund(ctx context.Context, req refunddomestic.CreateRequest) (*refunddomestic.Refund, error) {
	service := refunddomestic.RefundsApiService{Client: s.raw}
	response, _, err := service.Create(ctx, req)
	return response, err
}

func (s sdkRefundAPI) QueryRefund(ctx context.Context, req refunddomestic.QueryByOutRefundNoRequest) (*refunddomestic.Refund, error) {
	service := refunddomestic.RefundsApiService{Client: s.raw}
	response, _, err := service.QueryByOutRefundNo(ctx, req)
	return response, err
}
