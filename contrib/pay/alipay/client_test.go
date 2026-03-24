package alipay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-pay/gopay"
	gopayalipay "github.com/go-pay/gopay/alipay"
)

func TestNewValidation(t *testing.T) {
	_, err := New(nil)
	if !errors.Is(err, ErrNilConfig) {
		t.Fatalf("New(nil) error = %v, want %v", err, ErrNilConfig)
	}

	_, err = New(&Config{})
	if !errors.Is(err, ErrAppIDRequired) {
		t.Fatalf("New(empty) error = %v, want %v", err, ErrAppIDRequired)
	}

	_, err = New(&Config{AppID: "app"})
	if !errors.Is(err, ErrPrivateKeyRequired) {
		t.Fatalf("New(no private key) error = %v, want %v", err, ErrPrivateKeyRequired)
	}
}

func TestNewNormalizesAndClonesConfig(t *testing.T) {
	fake := &fakeAlipayClient{}
	cfg := &Config{
		AppID:           " app-id ",
		PrivateKey:      " private-key ",
		NotifyURL:       " https://notify.example.com ",
		ReturnURL:       " https://return.example.com ",
		Charset:         " utf-8 ",
		SignType:        " rsa2 ",
		AppAuthToken:    " token ",
		AlipayPublicKey: " public-key ",
		newClient: func(appID, privateKey string, isProd bool) (alipayAPI, error) {
			fake.appID = appID
			fake.privateKey = privateKey
			return fake, nil
		},
	}

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client == nil {
		t.Fatal("New() client = nil")
	}

	if got, want := fake.appID, "app-id"; got != want {
		t.Fatalf("appID = %q, want %q", got, want)
	}
	if got, want := fake.privateKey, "private-key"; got != want {
		t.Fatalf("privateKey = %q, want %q", got, want)
	}
	if got, want := fake.charset, gopayalipay.UTF8; got != want {
		t.Fatalf("charset = %q, want %q", got, want)
	}
	if got, want := fake.signType, gopayalipay.RSA2; got != want {
		t.Fatalf("signType = %q, want %q", got, want)
	}
	if got, want := fake.notifyURL, "https://notify.example.com"; got != want {
		t.Fatalf("notifyURL = %q, want %q", got, want)
	}
	if got, want := fake.returnURL, "https://return.example.com"; got != want {
		t.Fatalf("returnURL = %q, want %q", got, want)
	}
	if got, want := fake.appAuthToken, "token"; got != want {
		t.Fatalf("appAuthToken = %q, want %q", got, want)
	}

	cfg.AppID = "mutated"
	cfg.PrivateKey = "mutated"
	if got, want := fake.appID, "app-id"; got != want {
		t.Fatalf("factory appID after mutation = %q, want %q", got, want)
	}
}

func TestNewRejectsInvalidSignType(t *testing.T) {
	_, err := New(&Config{
		AppID:      "app-id",
		PrivateKey: "private-key",
		SignType:   "md5",
	})
	if !errors.Is(err, ErrInvalidSignType) {
		t.Fatalf("New() error = %v, want %v", err, ErrInvalidSignType)
	}
}

func TestNewConfiguresClient(t *testing.T) {
	fake := &fakeAlipayClient{}
	client, err := New(&Config{
		AppID:           "app-id",
		PrivateKey:      "private-key",
		NotifyURL:       "https://notify.example.com",
		ReturnURL:       "https://return.example.com",
		AppAuthToken:    "token",
		BodySizeMB:      20,
		AlipayPublicKey: "public-key",
		newClient: func(appID, privateKey string, isProd bool) (alipayAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client == nil {
		t.Fatal("New() client = nil")
	}
	if fake.notifyURL != "https://notify.example.com" {
		t.Fatalf("notifyURL = %q, want configured value", fake.notifyURL)
	}
	if fake.bodySize != 20 {
		t.Fatalf("bodySize = %d, want 20", fake.bodySize)
	}
}

func TestNewRejectsIncompleteCertificateConfig(t *testing.T) {
	_, err := New(&Config{
		AppID:      "app-id",
		PrivateKey: "private-key",
		Certificate: &CertificateConfig{
			AppCertPath: "app.crt",
		},
	})
	if !errors.Is(err, ErrIncompleteCertificateMode) {
		t.Fatalf("New() error = %v, want %v", err, ErrIncompleteCertificateMode)
	}
}

func TestTradeBillDownloadQuery(t *testing.T) {
	fake := &fakeAlipayClient{
		billResponse: &gopayalipay.DataBillDownloadUrlQueryResponse{
			Response: &gopayalipay.DataBillDownloadUrlQuery{BillDownloadUrl: "https://bill.example.com"},
		},
	}
	client, err := New(&Config{
		AppID:           "app-id",
		PrivateKey:      "private-key",
		AlipayPublicKey: "public-key",
		newClient: func(appID, privateKey string, isProd bool) (alipayAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	url, err := client.TradeBillDownloadQuery(context.Background(), gopay.BodyMap{})
	if err != nil {
		t.Fatalf("TradeBillDownloadQuery() error = %v", err)
	}
	if got, want := url, "https://bill.example.com"; got != want {
		t.Fatalf("TradeBillDownloadQuery() = %q, want %q", got, want)
	}
}

func TestTradeBillDownloadQueryValidation(t *testing.T) {
	fake := &fakeAlipayClient{}
	client, err := New(&Config{
		AppID:           "app-id",
		PrivateKey:      "private-key",
		AlipayPublicKey: "public-key",
		newClient: func(appID, privateKey string, isProd bool) (alipayAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.TradeBillDownloadQuery(nil, gopay.BodyMap{}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("TradeBillDownloadQuery(nil) error = %v, want %v", err, ErrContextRequired)
	}
	if _, err := client.TradeBillDownloadQuery(context.Background(), gopay.BodyMap{}); !errors.Is(err, ErrBillDownloadURLEmpty) {
		t.Fatalf("TradeBillDownloadQuery(empty url) error = %v, want %v", err, ErrBillDownloadURLEmpty)
	}
}

func TestParseNotifyRequiresVerifierConfig(t *testing.T) {
	fake := &fakeAlipayClient{}
	client, err := New(&Config{
		AppID:      "app-id",
		PrivateKey: "private-key",
		newClient: func(appID, privateKey string, isProd bool) (alipayAPI, error) {
			return fake, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := httptest.NewRequest("POST", "/notify", strings.NewReader("out_trade_no=123&sign=test"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = client.ParseNotify(req)
	if !errors.Is(err, ErrVerifyConfigRequired) {
		t.Fatalf("ParseNotify() error = %v, want %v", err, ErrVerifyConfigRequired)
	}
}

func TestParseNotifyValidationAndSuccess(t *testing.T) {
	bodyMap := gopay.BodyMap{"out_trade_no": "123"}
	client, err := New(&Config{
		AppID:           "app-id",
		PrivateKey:      "private-key",
		AlipayPublicKey: "public-key",
		parseNotify: func(req *http.Request) (gopay.BodyMap, error) {
			return bodyMap, nil
		},
		verifySign: func(publicKey string, got any) (bool, error) {
			parsed, ok := got.(gopay.BodyMap)
			if !ok {
				t.Fatalf("verify body map type = %T, want gopay.BodyMap", got)
			}
			if publicKey != "public-key" {
				t.Fatalf("verify public key = %q, want public-key", publicKey)
			}
			if parsed["out_trade_no"] != "123" {
				t.Fatalf("verify body map = %#v", parsed)
			}
			return true, nil
		},
		newClient: func(appID, privateKey string, isProd bool) (alipayAPI, error) {
			return &fakeAlipayClient{}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.ParseNotify(nil); !errors.Is(err, ErrRequestRequired) {
		t.Fatalf("ParseNotify(nil) error = %v, want %v", err, ErrRequestRequired)
	}

	req := httptest.NewRequest("POST", "/notify", strings.NewReader("ignored=1"))
	got, err := client.ParseNotify(req)
	if err != nil {
		t.Fatalf("ParseNotify() error = %v", err)
	}
	if got["out_trade_no"] != "123" {
		t.Fatalf("ParseNotify() body map = %#v", got)
	}
}

type fakeAlipayClient struct {
	appID        string
	privateKey   string
	charset      string
	signType     string
	notifyURL    string
	returnURL    string
	appAuthToken string
	bodySize     int
	billResponse *gopayalipay.DataBillDownloadUrlQueryResponse
}

func (f *fakeAlipayClient) SetCharset(value string) *gopayalipay.Client {
	f.charset = value
	return nil
}
func (f *fakeAlipayClient) SetSignType(value string) *gopayalipay.Client {
	f.signType = value
	return nil
}
func (f *fakeAlipayClient) SetNotifyUrl(url string) *gopayalipay.Client {
	f.notifyURL = url
	return nil
}
func (f *fakeAlipayClient) SetReturnUrl(value string) *gopayalipay.Client {
	f.returnURL = value
	return nil
}
func (f *fakeAlipayClient) SetAppAuthToken(value string) *gopayalipay.Client {
	f.appAuthToken = value
	return nil
}
func (f *fakeAlipayClient) SetBodySize(size int)                         { f.bodySize = size }
func (f *fakeAlipayClient) SetCertSnByPath(string, string, string) error { return nil }
func (f *fakeAlipayClient) TradePagePay(context.Context, gopay.BodyMap) (string, error) {
	return "", nil
}
func (f *fakeAlipayClient) TradeWapPay(context.Context, gopay.BodyMap) (string, error) {
	return "", nil
}
func (f *fakeAlipayClient) TradeAppPay(context.Context, gopay.BodyMap) (string, error) {
	return "", nil
}
func (f *fakeAlipayClient) TradePrecreate(context.Context, gopay.BodyMap) (*gopayalipay.TradePrecreateResponse, error) {
	return nil, nil
}
func (f *fakeAlipayClient) TradePay(context.Context, gopay.BodyMap) (*gopayalipay.TradePayResponse, error) {
	return nil, nil
}
func (f *fakeAlipayClient) TradeQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeQueryResponse, error) {
	return nil, nil
}
func (f *fakeAlipayClient) TradeClose(context.Context, gopay.BodyMap) (*gopayalipay.TradeCloseResponse, error) {
	return nil, nil
}
func (f *fakeAlipayClient) TradeRefund(context.Context, gopay.BodyMap) (*gopayalipay.TradeRefundResponse, error) {
	return nil, nil
}
func (f *fakeAlipayClient) TradeFastPayRefundQuery(context.Context, gopay.BodyMap) (*gopayalipay.TradeFastpayRefundQueryResponse, error) {
	return nil, nil
}
func (f *fakeAlipayClient) DataBillDownloadUrlQuery(context.Context, gopay.BodyMap) (*gopayalipay.DataBillDownloadUrlQueryResponse, error) {
	return f.billResponse, nil
}
