package alipay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bang-go/util"
	"github.com/go-pay/gopay"
	gopayalipay "github.com/go-pay/gopay/alipay"
)

var (
	ErrNilConfig                 = errors.New("alipay: config is required")
	ErrContextRequired           = errors.New("alipay: context is required")
	ErrRequestRequired           = errors.New("alipay: request is required")
	ErrAppIDRequired             = errors.New("alipay: app id is required")
	ErrPrivateKeyRequired        = errors.New("alipay: private key is required")
	ErrInvalidSignType           = errors.New("alipay: sign type must be RSA or RSA2")
	ErrIncompleteCertificateMode = errors.New("alipay: app/public/root certificate paths must all be provided")
	ErrVerifyConfigRequired      = errors.New("alipay: notify verification requires either certificate mode or alipay public key")
	ErrNotifyVerifyFailed        = errors.New("alipay: notify signature verification failed")
	ErrBillDownloadURLEmpty      = errors.New("alipay: bill download url is empty")
)

type CertificateConfig struct {
	AppCertPath          string
	RootCertPath         string
	AlipayPublicCertPath string
}

type Config struct {
	AppID           string
	PrivateKey      string
	IsProduction    bool
	NotifyURL       string
	ReturnURL       string
	Charset         string
	SignType        string
	AppAuthToken    string
	BodySizeMB      int
	AlipayPublicKey string
	Certificate     *CertificateConfig

	newClient          func(appID, privateKey string, isProd bool) (alipayAPI, error)
	parseNotify        func(*http.Request) (gopay.BodyMap, error)
	verifySign         func(string, any) (bool, error)
	verifySignWithCert func(string, any) (bool, error)
}

type Client interface {
	Raw() *gopayalipay.Client
	TradePagePay(context.Context, gopay.BodyMap) (string, error)
	TradeWapPay(context.Context, gopay.BodyMap) (string, error)
	TradeAppPay(context.Context, gopay.BodyMap) (string, error)
	TradePrecreate(context.Context, gopay.BodyMap) (*gopayalipay.TradePrecreateResponse, error)
	TradePay(context.Context, gopay.BodyMap) (*gopayalipay.TradePayResponse, error)
	TradeQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeQueryResponse, error)
	TradeClose(context.Context, gopay.BodyMap) (*gopayalipay.TradeCloseResponse, error)
	TradeRefund(context.Context, gopay.BodyMap) (*gopayalipay.TradeRefundResponse, error)
	TradeRefundQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeFastpayRefundQueryResponse, error)
	TradeBillDownloadQuery(context.Context, gopay.BodyMap) (string, error)
	ParseNotify(*http.Request) (gopay.BodyMap, error)
}

type alipayAPI interface {
	SetCharset(string) *gopayalipay.Client
	SetSignType(string) *gopayalipay.Client
	SetNotifyUrl(string) *gopayalipay.Client
	SetReturnUrl(string) *gopayalipay.Client
	SetAppAuthToken(string) *gopayalipay.Client
	SetBodySize(int)
	SetCertSnByPath(string, string, string) error
	TradePagePay(context.Context, gopay.BodyMap) (string, error)
	TradeWapPay(context.Context, gopay.BodyMap) (string, error)
	TradeAppPay(context.Context, gopay.BodyMap) (string, error)
	TradePrecreate(context.Context, gopay.BodyMap) (*gopayalipay.TradePrecreateResponse, error)
	TradePay(context.Context, gopay.BodyMap) (*gopayalipay.TradePayResponse, error)
	TradeQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeQueryResponse, error)
	TradeClose(context.Context, gopay.BodyMap) (*gopayalipay.TradeCloseResponse, error)
	TradeRefund(context.Context, gopay.BodyMap) (*gopayalipay.TradeRefundResponse, error)
	TradeFastPayRefundQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeFastpayRefundQueryResponse, error)
	DataBillDownloadUrlQuery(context.Context, gopay.BodyMap) (*gopayalipay.DataBillDownloadUrlQueryResponse, error)
}

type client struct {
	raw    *gopayalipay.Client
	api    alipayAPI
	config *Config
}

func New(cfg *Config) (Client, error) {
	config, err := prepareConfig(cfg)
	if err != nil {
		return nil, err
	}

	factory := config.newClient
	if factory == nil {
		factory = func(appID, privateKey string, isProd bool) (alipayAPI, error) {
			return gopayalipay.NewClient(appID, privateKey, isProd)
		}
	}

	api, err := factory(config.AppID, config.PrivateKey, config.IsProduction)
	if err != nil {
		return nil, fmt.Errorf("alipay: create client failed: %w", err)
	}

	api.SetCharset(config.Charset)
	api.SetSignType(config.SignType)
	if config.NotifyURL != "" {
		api.SetNotifyUrl(config.NotifyURL)
	}
	if config.ReturnURL != "" {
		api.SetReturnUrl(config.ReturnURL)
	}
	if config.AppAuthToken != "" {
		api.SetAppAuthToken(config.AppAuthToken)
	}
	if config.BodySizeMB > 0 {
		api.SetBodySize(config.BodySizeMB)
	}
	if config.Certificate != nil {
		if err := api.SetCertSnByPath(config.Certificate.AppCertPath, config.Certificate.RootCertPath, config.Certificate.AlipayPublicCertPath); err != nil {
			return nil, fmt.Errorf("alipay: configure certificates failed: %w", err)
		}
	}

	raw, _ := api.(*gopayalipay.Client)
	return &client{raw: raw, api: api, config: config}, nil
}

func (c *client) Raw() *gopayalipay.Client {
	return c.raw
}

func (c *client) TradePagePay(ctx context.Context, bm gopay.BodyMap) (string, error) {
	if ctx == nil {
		return "", ErrContextRequired
	}
	return c.api.TradePagePay(ctx, bm)
}

func (c *client) TradeWapPay(ctx context.Context, bm gopay.BodyMap) (string, error) {
	if ctx == nil {
		return "", ErrContextRequired
	}
	return c.api.TradeWapPay(ctx, bm)
}

func (c *client) TradeAppPay(ctx context.Context, bm gopay.BodyMap) (string, error) {
	if ctx == nil {
		return "", ErrContextRequired
	}
	return c.api.TradeAppPay(ctx, bm)
}

func (c *client) TradePrecreate(ctx context.Context, bm gopay.BodyMap) (*gopayalipay.TradePrecreateResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	return c.api.TradePrecreate(ctx, bm)
}

func (c *client) TradePay(ctx context.Context, bm gopay.BodyMap) (*gopayalipay.TradePayResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	return c.api.TradePay(ctx, bm)
}

func (c *client) TradeQuery(ctx context.Context, bm gopay.BodyMap) (*gopayalipay.TradeQueryResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	return c.api.TradeQuery(ctx, bm)
}

func (c *client) TradeClose(ctx context.Context, bm gopay.BodyMap) (*gopayalipay.TradeCloseResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	return c.api.TradeClose(ctx, bm)
}

func (c *client) TradeRefund(ctx context.Context, bm gopay.BodyMap) (*gopayalipay.TradeRefundResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	return c.api.TradeRefund(ctx, bm)
}

func (c *client) TradeRefundQuery(ctx context.Context, bm gopay.BodyMap) (*gopayalipay.TradeFastpayRefundQueryResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	return c.api.TradeFastPayRefundQuery(ctx, bm)
}

func (c *client) TradeBillDownloadQuery(ctx context.Context, bm gopay.BodyMap) (string, error) {
	if ctx == nil {
		return "", ErrContextRequired
	}
	response, err := c.api.DataBillDownloadUrlQuery(ctx, bm)
	if err != nil {
		return "", err
	}
	if response == nil || response.Response == nil || response.Response.BillDownloadUrl == "" {
		return "", ErrBillDownloadURLEmpty
	}
	return response.Response.BillDownloadUrl, nil
}

func (c *client) ParseNotify(req *http.Request) (gopay.BodyMap, error) {
	if req == nil {
		return nil, ErrRequestRequired
	}

	parseNotify := c.config.parseNotify
	if parseNotify == nil {
		parseNotify = gopayalipay.ParseNotifyToBodyMap
	}

	bodyMap, err := parseNotify(req)
	if err != nil {
		return nil, err
	}

	ok, err := c.verifyNotify(bodyMap)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotifyVerifyFailed
	}
	return bodyMap, nil
}

func (c *client) verifyNotify(bodyMap gopay.BodyMap) (bool, error) {
	if c.config.Certificate != nil {
		verifyWithCert := c.config.verifySignWithCert
		if verifyWithCert == nil {
			verifyWithCert = func(certPath string, notify any) (bool, error) {
				return gopayalipay.VerifySignWithCert(certPath, notify)
			}
		}
		return verifyWithCert(c.config.Certificate.AlipayPublicCertPath, bodyMap)
	}
	if c.config.AlipayPublicKey != "" {
		verifySign := c.config.verifySign
		if verifySign == nil {
			verifySign = gopayalipay.VerifySign
		}
		return verifySign(c.config.AlipayPublicKey, bodyMap)
	}
	return false, ErrVerifyConfigRequired
}

func prepareConfig(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	cloned := *cfg
	cloned.AppID = strings.TrimSpace(cloned.AppID)
	cloned.PrivateKey = strings.TrimSpace(cloned.PrivateKey)
	cloned.NotifyURL = strings.TrimSpace(cloned.NotifyURL)
	cloned.ReturnURL = strings.TrimSpace(cloned.ReturnURL)
	cloned.Charset = strings.TrimSpace(cloned.Charset)
	cloned.SignType = strings.ToUpper(strings.TrimSpace(cloned.SignType))
	cloned.AppAuthToken = strings.TrimSpace(cloned.AppAuthToken)
	cloned.AlipayPublicKey = strings.TrimSpace(cloned.AlipayPublicKey)

	switch {
	case cloned.AppID == "":
		return nil, ErrAppIDRequired
	case cloned.PrivateKey == "":
		return nil, ErrPrivateKeyRequired
	}

	if cloned.Charset == "" {
		cloned.Charset = gopayalipay.UTF8
	}
	if cloned.SignType == "" {
		cloned.SignType = gopayalipay.RSA2
	}
	switch cloned.SignType {
	case gopayalipay.RSA, gopayalipay.RSA2:
	default:
		return nil, ErrInvalidSignType
	}

	if cloned.Certificate != nil {
		certificate := util.ClonePtr(cloned.Certificate)
		certificate.AppCertPath = strings.TrimSpace(certificate.AppCertPath)
		certificate.RootCertPath = strings.TrimSpace(certificate.RootCertPath)
		certificate.AlipayPublicCertPath = strings.TrimSpace(certificate.AlipayPublicCertPath)
		if certificate.AppCertPath == "" || certificate.RootCertPath == "" || certificate.AlipayPublicCertPath == "" {
			return nil, ErrIncompleteCertificateMode
		}
		cloned.Certificate = certificate
	}

	return &cloned, nil
}
