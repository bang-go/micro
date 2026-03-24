package wechat

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"net/http"
	"testing"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/app"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/h5"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
)

func TestPrepareConfig(t *testing.T) {
	t.Run("validate required fields", func(t *testing.T) {
		_, err := prepareConfig(nil)
		if !errors.Is(err, ErrNilConfig) {
			t.Fatalf("expected ErrNilConfig, got %v", err)
		}

		_, err = prepareConfig(&Config{})
		if !errors.Is(err, ErrAppIDRequired) {
			t.Fatalf("expected ErrAppIDRequired, got %v", err)
		}
	})

	t.Run("trim and populate defaults", func(t *testing.T) {
		cfg, err := prepareConfig(&Config{
			AppID:                      " app ",
			MchID:                      " mch ",
			MchCertificateSerialNumber: " serial ",
			MchAPIv3Key:                " key ",
			MchPrivateKeyPath:          " path ",
			NotifyURL:                  " https://notify.example.com ",
		})
		if err != nil {
			t.Fatalf("prepareConfig() error = %v", err)
		}
		if cfg.AppID != "app" || cfg.MchID != "mch" || cfg.NotifyURL != "https://notify.example.com" {
			t.Fatalf("prepareConfig() did not trim config: %+v", cfg)
		}
		if cfg.loadPrivateKey == nil || cfg.newClient == nil || cfg.newNotifyHandler == nil || cfg.newPayments == nil || cfg.newRefunds == nil {
			t.Fatal("prepareConfig() did not populate internal defaults")
		}
	})
}

func TestNew(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}

	t.Run("reject nil context", func(t *testing.T) {
		_, err := New(nil, &Config{
			AppID:                      "app",
			MchID:                      "mch",
			MchCertificateSerialNumber: "serial",
			MchAPIv3Key:                "12345678901234567890123456789012",
			MchPrivateKeyPath:          "/tmp/mch.pem",
		})
		if !errors.Is(err, ErrContextRequired) {
			t.Fatalf("expected ErrContextRequired, got %v", err)
		}
	})

	t.Run("fail on downloader registration", func(t *testing.T) {
		expected := errors.New("register failed")
		_, err := New(context.Background(), &Config{
			AppID:                      "app",
			MchID:                      "mch",
			MchCertificateSerialNumber: "serial",
			MchAPIv3Key:                "12345678901234567890123456789012",
			MchPrivateKeyPath:          "/tmp/mch.pem",
			loadPrivateKey: func(string) (*rsa.PrivateKey, error) {
				return privateKey, nil
			},
			newClient: func(context.Context, ...core.ClientOption) (*core.Client, error) {
				return &core.Client{}, nil
			},
			downloader: &fakeCertificateRegistry{registerErr: expected},
			newNotifyHandler: func(string, auth.Verifier) (notifyParser, error) {
				return fakeNotifyParser{}, nil
			},
			newPayments: func(*core.Client) paymentAPI { return &fakePaymentAPI{} },
			newRefunds:  func(*core.Client) refundAPI { return &fakeRefundAPI{} },
		})
		if !errors.Is(err, expected) {
			t.Fatalf("expected downloader error, got %v", err)
		}
	})

	t.Run("build client with injected collaborators", func(t *testing.T) {
		fakePayments := &fakePaymentAPI{}
		fakeRefunds := &fakeRefundAPI{}
		fakeNotify := fakeNotifyParser{}

		client, err := New(context.Background(), &Config{
			AppID:                      "app",
			MchID:                      "mch",
			MchCertificateSerialNumber: "serial",
			MchAPIv3Key:                "12345678901234567890123456789012",
			MchPrivateKeyPath:          "/tmp/mch.pem",
			loadPrivateKey: func(string) (*rsa.PrivateKey, error) {
				return privateKey, nil
			},
			newClient: func(context.Context, ...core.ClientOption) (*core.Client, error) {
				return &core.Client{}, nil
			},
			downloader: &fakeCertificateRegistry{},
			newNotifyHandler: func(string, auth.Verifier) (notifyParser, error) {
				return fakeNotify, nil
			},
			newPayments: func(*core.Client) paymentAPI { return fakePayments },
			newRefunds:  func(*core.Client) refundAPI { return fakeRefunds },
		}, WithHTTPClient(http.DefaultClient))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if client.Raw() == nil || client.GetClient() == nil {
			t.Fatal("expected raw client to be available")
		}
	})
}

func TestPrepayDefaults(t *testing.T) {
	fakePayments := &fakePaymentAPI{}
	cli := &client{
		config: &Config{
			AppID:     "app-id",
			MchID:     "mch-id",
			NotifyURL: "https://notify.example.com",
		},
		payments: fakePayments,
		refunds:  &fakeRefundAPI{},
	}

	if _, err := cli.JsapiPrepay(context.Background(), jsapi.PrepayRequest{}); err != nil {
		t.Fatalf("JsapiPrepay() error = %v", err)
	}
	if got := stringValue(fakePayments.jsapiReq.Appid); got != "app-id" {
		t.Fatalf("expected jsapi appid, got %q", got)
	}
	if got := stringValue(fakePayments.jsapiReq.Mchid); got != "mch-id" {
		t.Fatalf("expected jsapi mchid, got %q", got)
	}
	if got := stringValue(fakePayments.jsapiReq.NotifyUrl); got != "https://notify.example.com" {
		t.Fatalf("expected jsapi notify url, got %q", got)
	}

	if _, err := cli.NativePrepay(context.Background(), native.PrepayRequest{}); err != nil {
		t.Fatalf("NativePrepay() error = %v", err)
	}
	if got := stringValue(fakePayments.nativeReq.Appid); got != "app-id" {
		t.Fatalf("expected native appid, got %q", got)
	}

	if _, err := cli.AppPrepay(context.Background(), app.PrepayRequest{}); err != nil {
		t.Fatalf("AppPrepay() error = %v", err)
	}
	if got := stringValue(fakePayments.appReq.Appid); got != "app-id" {
		t.Fatalf("expected app appid, got %q", got)
	}

	if _, err := cli.H5Prepay(context.Background(), h5.PrepayRequest{}); err != nil {
		t.Fatalf("H5Prepay() error = %v", err)
	}
	if got := stringValue(fakePayments.h5Req.Appid); got != "app-id" {
		t.Fatalf("expected h5 appid, got %q", got)
	}

	if _, err := cli.JsapiPrepay(context.Background(), jsapi.PrepayRequest{
		Appid:     core.String(" "),
		Mchid:     core.String(" mch-custom "),
		NotifyUrl: core.String(" "),
	}); err != nil {
		t.Fatalf("JsapiPrepay(blank values) error = %v", err)
	}
	if got := stringValue(fakePayments.jsapiReq.Appid); got != "app-id" {
		t.Fatalf("expected blank appid to default, got %q", got)
	}
	if got := stringValue(fakePayments.jsapiReq.Mchid); got != "mch-custom" {
		t.Fatalf("expected mchid to be trimmed, got %q", got)
	}
	if got := stringValue(fakePayments.jsapiReq.NotifyUrl); got != "https://notify.example.com" {
		t.Fatalf("expected blank notify url to default, got %q", got)
	}

	noNotifyClient := &client{
		config: &Config{
			AppID: "app-id",
			MchID: "mch-id",
		},
		payments: fakePayments,
	}
	if _, err := noNotifyClient.JsapiPrepay(context.Background(), jsapi.PrepayRequest{
		NotifyUrl: core.String(" "),
	}); err != nil {
		t.Fatalf("JsapiPrepay(no notify default) error = %v", err)
	}
	if fakePayments.jsapiReq.NotifyUrl != nil {
		t.Fatalf("expected notify url to be omitted, got %q", stringValue(fakePayments.jsapiReq.NotifyUrl))
	}
}

func TestOrderRefundAndNotify(t *testing.T) {
	fakePayments := &fakePaymentAPI{}
	fakeRefunds := &fakeRefundAPI{}
	fakeNotify := &capturingNotifyParser{}
	cli := &client{
		config:   &Config{MchID: "merchant"},
		payments: fakePayments,
		refunds:  fakeRefunds,
		handler:  fakeNotify,
	}

	if _, err := cli.QueryOrderByOutTradeNo(context.Background(), " "); !errors.Is(err, ErrOutTradeNoRequired) {
		t.Fatalf("expected ErrOutTradeNoRequired, got %v", err)
	}
	if _, err := cli.QueryOrderByOutTradeNo(nil, "trade-0"); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if err := cli.CloseOrder(context.Background(), " "); !errors.Is(err, ErrOutTradeNoRequired) {
		t.Fatalf("expected ErrOutTradeNoRequired, got %v", err)
	}
	if err := cli.CloseOrder(nil, "trade-0"); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := cli.Refund(nil, refunddomestic.CreateRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := cli.QueryRefund(context.Background(), " "); !errors.Is(err, ErrOutRefundNoRequired) {
		t.Fatalf("expected ErrOutRefundNoRequired, got %v", err)
	}
	if _, err := cli.QueryRefund(nil, "refund-0"); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}

	if _, err := cli.QueryOrderByOutTradeNo(context.Background(), "trade-1"); err != nil {
		t.Fatalf("QueryOrderByOutTradeNo() error = %v", err)
	}
	if got := stringValue(fakePayments.queryOrderReq.Mchid); got != "merchant" {
		t.Fatalf("expected mchid on query request, got %q", got)
	}

	if err := cli.CloseOrder(context.Background(), "trade-2"); err != nil {
		t.Fatalf("CloseOrder() error = %v", err)
	}
	if got := stringValue(fakePayments.closeOrderReq.Mchid); got != "merchant" {
		t.Fatalf("expected mchid on close request, got %q", got)
	}

	if _, err := cli.QueryRefund(context.Background(), "refund-1"); err != nil {
		t.Fatalf("QueryRefund() error = %v", err)
	}
	if got := stringValue(fakeRefunds.queryReq.OutRefundNo); got != "refund-1" {
		t.Fatalf("expected refund number, got %q", got)
	}

	ctx := context.WithValue(context.Background(), testContextKey("trace"), "value")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/notify", nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	if _, err := cli.ParseNotify(req, map[string]any{}); err != nil {
		t.Fatalf("ParseNotify() error = %v", err)
	}
	if fakeNotify.ctxValue != "value" {
		t.Fatalf("expected request context to flow into parser, got %q", fakeNotify.ctxValue)
	}

	if _, err := (&client{}).ParseNotify(nil, nil); !errors.Is(err, ErrNotifyRequestRequired) {
		t.Fatalf("expected ErrNotifyRequestRequired, got %v", err)
	}
	if _, err := (&client{}).ParseNotify(req, nil); !errors.Is(err, ErrNotifyHandlerUninitialized) {
		t.Fatalf("expected ErrNotifyHandlerUninitialized, got %v", err)
	}
}

func TestPrepayRequiresContext(t *testing.T) {
	cli := &client{
		config: &Config{
			AppID:     "app-id",
			MchID:     "mch-id",
			NotifyURL: "https://notify.example.com",
		},
		payments: &fakePaymentAPI{},
	}

	if _, err := cli.JsapiPrepay(nil, jsapi.PrepayRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := cli.NativePrepay(nil, native.PrepayRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := cli.AppPrepay(nil, app.PrepayRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
	if _, err := cli.H5Prepay(nil, h5.PrepayRequest{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("expected ErrContextRequired, got %v", err)
	}
}

type testContextKey string

type fakeCertificateRegistry struct {
	registerErr error
}

func (f *fakeCertificateRegistry) RegisterDownloaderWithClient(context.Context, *core.Client, string, string) error {
	return f.registerErr
}

func (f *fakeCertificateRegistry) GetCertificateVisitor(string) core.CertificateVisitor {
	return fakeCertificateVisitor{}
}

type fakeNotifyParser struct{}

func (fakeNotifyParser) ParseNotifyRequest(context.Context, *http.Request, any) (*notify.Request, error) {
	return &notify.Request{}, nil
}

type capturingNotifyParser struct {
	ctxValue string
}

func (c *capturingNotifyParser) ParseNotifyRequest(ctx context.Context, _ *http.Request, _ any) (*notify.Request, error) {
	if value, _ := ctx.Value(testContextKey("trace")).(string); value != "" {
		c.ctxValue = value
	}
	return &notify.Request{}, nil
}

type fakePaymentAPI struct {
	jsapiReq      jsapi.PrepayRequest
	nativeReq     native.PrepayRequest
	appReq        app.PrepayRequest
	h5Req         h5.PrepayRequest
	queryOrderReq jsapi.QueryOrderByOutTradeNoRequest
	closeOrderReq jsapi.CloseOrderRequest
}

func (f *fakePaymentAPI) JsapiPrepay(_ context.Context, req jsapi.PrepayRequest) (*jsapi.PrepayWithRequestPaymentResponse, error) {
	f.jsapiReq = req
	return &jsapi.PrepayWithRequestPaymentResponse{}, nil
}

func (f *fakePaymentAPI) NativePrepay(_ context.Context, req native.PrepayRequest) (*native.PrepayResponse, error) {
	f.nativeReq = req
	return &native.PrepayResponse{}, nil
}

func (f *fakePaymentAPI) AppPrepay(_ context.Context, req app.PrepayRequest) (*app.PrepayWithRequestPaymentResponse, error) {
	f.appReq = req
	return &app.PrepayWithRequestPaymentResponse{}, nil
}

func (f *fakePaymentAPI) H5Prepay(_ context.Context, req h5.PrepayRequest) (*h5.PrepayResponse, error) {
	f.h5Req = req
	return &h5.PrepayResponse{}, nil
}

func (f *fakePaymentAPI) QueryOrderByOutTradeNo(_ context.Context, req jsapi.QueryOrderByOutTradeNoRequest) (*payments.Transaction, error) {
	f.queryOrderReq = req
	return &payments.Transaction{}, nil
}

func (f *fakePaymentAPI) CloseOrder(_ context.Context, req jsapi.CloseOrderRequest) error {
	f.closeOrderReq = req
	return nil
}

type fakeRefundAPI struct {
	queryReq refunddomestic.QueryByOutRefundNoRequest
}

type fakeCertificateVisitor struct{}

func (fakeCertificateVisitor) Get(context.Context, string) (*x509.Certificate, bool) {
	return nil, false
}

func (fakeCertificateVisitor) GetAll(context.Context) map[string]*x509.Certificate {
	return nil
}

func (fakeCertificateVisitor) GetNewestSerial(context.Context) string {
	return ""
}

func (fakeCertificateVisitor) Export(context.Context, string) (string, bool) {
	return "", false
}

func (fakeCertificateVisitor) ExportAll(context.Context) map[string]string {
	return nil
}

func (f *fakeRefundAPI) Refund(context.Context, refunddomestic.CreateRequest) (*refunddomestic.Refund, error) {
	return &refunddomestic.Refund{}, nil
}

func (f *fakeRefundAPI) QueryRefund(_ context.Context, req refunddomestic.QueryByOutRefundNoRequest) (*refunddomestic.Refund, error) {
	f.queryReq = req
	return &refunddomestic.Refund{}, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
