package sms

import (
	"context"
	"errors"
	"testing"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/models"
	teaUtil "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/bang-go/util"
)

func TestPrepareConfig(t *testing.T) {
	t.Run("validate required config", func(t *testing.T) {
		_, err := prepareConfig(nil)
		if !errors.Is(err, ErrNilConfig) {
			t.Fatalf("expected ErrNilConfig, got %v", err)
		}

		_, err = prepareConfig(&Config{})
		if !errors.Is(err, ErrAccessKeyIDRequired) {
			t.Fatalf("expected ErrAccessKeyIDRequired, got %v", err)
		}
	})

	t.Run("trim values and keep defaults", func(t *testing.T) {
		cfg, err := prepareConfig(&Config{
			AccessKeyID:     " ak ",
			AccessKeySecret: " sk ",
			Endpoint:        " dysmsapi.aliyuncs.com ",
			RegionID:        " cn-hangzhou ",
			UserAgent:       " custom-agent ",
		})
		if err != nil {
			t.Fatalf("prepareConfig() error = %v", err)
		}
		if cfg.AccessKeyID != "ak" || cfg.AccessKeySecret != "sk" || cfg.Endpoint != "dysmsapi.aliyuncs.com" || cfg.RegionID != "cn-hangzhou" {
			t.Fatalf("prepareConfig() did not trim config: %+v", cfg)
		}
		if cfg.newClient == nil {
			t.Fatal("prepareConfig() did not populate client factory")
		}
	})
}

func TestNew(t *testing.T) {
	var captured *openapi.Config
	fakeAPI := &fakeSMSAPI{}

	client, err := New(&Config{
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		Endpoint:        "endpoint",
		RegionID:        "region",
		SecurityToken:   "token",
		UserAgent:       "agent",
		newClient: func(cfg *openapi.Config) (smsAPI, error) {
			captured = cfg
			return fakeAPI, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client.Raw() != nil {
		t.Fatal("expected Raw() to be nil when using a fake api")
	}
	if captured == nil {
		t.Fatal("expected openapi config to be passed to factory")
	}
	if util.DerefZero(captured.AccessKeyId) != "ak" || util.DerefZero(captured.RegionId) != "region" || util.DerefZero(captured.SecurityToken) != "token" {
		t.Fatalf("unexpected openapi config: %+v", captured)
	}
}

func TestClientOperations(t *testing.T) {
	fakeAPI := &fakeSMSAPI{}
	client, err := New(&Config{
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		newClient: func(*openapi.Config) (smsAPI, error) {
			return fakeAPI, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("send sms validates request", func(t *testing.T) {
		_, err := client.SendSms(context.Background(), nil)
		if !errors.Is(err, ErrSendRequestRequired) {
			t.Fatalf("expected ErrSendRequestRequired, got %v", err)
		}
		_, err = client.SendSms(nil, &SendSmsRequest{
			PhoneNumbers: util.Ptr("13800000000"),
			SignName:     util.Ptr("bang"),
			TemplateCode: util.Ptr("SMS_1"),
		})
		if !errors.Is(err, ErrContextRequired) {
			t.Fatalf("expected ErrContextRequired, got %v", err)
		}

		request := &SendSmsRequest{
			PhoneNumbers: util.Ptr(" 13800000000 "),
			SignName:     util.Ptr(" bang "),
			TemplateCode: util.Ptr(" SMS_1 "),
		}
		_, err = client.SendSms(context.Background(), request)
		if err != nil {
			t.Fatalf("SendSms() error = %v", err)
		}
		if fakeAPI.sendSMS.phone != "13800000000" || fakeAPI.sendSMS.template != "SMS_1" {
			t.Fatalf("unexpected request forwarding: %+v", fakeAPI.sendSMS)
		}
		if got, want := util.DerefZero(request.PhoneNumbers), " 13800000000 "; got != want {
			t.Fatalf("original request phone = %q, want %q", got, want)
		}
		if got, want := util.DerefZero(request.TemplateCode), " SMS_1 "; got != want {
			t.Fatalf("original request template = %q, want %q", got, want)
		}
	})

	t.Run("send sms with options forwards context and runtime", func(t *testing.T) {
		runtime := &teaUtil.RuntimeOptions{}
		ctx := context.WithValue(context.Background(), testContextKey("trace"), "value")
		_, err := client.SendSmsWithOptions(ctx, &SendSmsRequest{
			PhoneNumbers: util.Ptr("13800000001"),
			SignName:     util.Ptr("bang"),
			TemplateCode: util.Ptr("SMS_2"),
		}, runtime)
		if err != nil {
			t.Fatalf("SendSmsWithOptions() error = %v", err)
		}
		if fakeAPI.sendSMS.ctxValue != "value" || fakeAPI.sendSMS.runtime != runtime {
			t.Fatalf("expected context/runtime forwarding, got %+v", fakeAPI.sendSMS)
		}
	})

	t.Run("batch sms validates request", func(t *testing.T) {
		_, err := client.SendBatchSms(context.Background(), nil)
		if !errors.Is(err, ErrBatchRequestRequired) {
			t.Fatalf("expected ErrBatchRequestRequired, got %v", err)
		}
		_, err = client.SendBatchSms(nil, &SendBatchSmsRequest{
			PhoneNumberJson: util.Ptr("[\"13800000000\"]"),
			SignNameJson:    util.Ptr("[\"bang\"]"),
			TemplateCode:    util.Ptr("SMS_BATCH"),
		})
		if !errors.Is(err, ErrContextRequired) {
			t.Fatalf("expected ErrContextRequired, got %v", err)
		}

		request := &SendBatchSmsRequest{
			PhoneNumberJson: util.Ptr(" [\"13800000000\"] "),
			SignNameJson:    util.Ptr(" [\"bang\"] "),
			TemplateCode:    util.Ptr(" SMS_BATCH "),
		}
		_, err = client.SendBatchSms(context.Background(), request)
		if err != nil {
			t.Fatalf("SendBatchSms() error = %v", err)
		}
		if fakeAPI.batch.template != "SMS_BATCH" {
			t.Fatalf("unexpected batch forwarding: %+v", fakeAPI.batch)
		}
		if got, want := util.DerefZero(request.TemplateCode), " SMS_BATCH "; got != want {
			t.Fatalf("original batch template = %q, want %q", got, want)
		}
	})

	t.Run("query details validates request", func(t *testing.T) {
		_, err := client.QuerySendDetails(context.Background(), nil)
		if !errors.Is(err, ErrQueryRequestRequired) {
			t.Fatalf("expected ErrQueryRequestRequired, got %v", err)
		}
		_, err = client.QuerySendDetails(nil, &QuerySendDetailsRequest{
			PhoneNumber: util.Ptr("13800000000"),
			SendDate:    util.Ptr("20260323"),
		})
		if !errors.Is(err, ErrContextRequired) {
			t.Fatalf("expected ErrContextRequired, got %v", err)
		}

		request := &QuerySendDetailsRequest{
			PhoneNumber: util.Ptr(" 13800000000 "),
			SendDate:    util.Ptr(" 20260323 "),
		}
		_, err = client.QuerySendDetails(context.Background(), request)
		if err != nil {
			t.Fatalf("QuerySendDetails() error = %v", err)
		}
		if fakeAPI.query.phone != "13800000000" || fakeAPI.query.date != "20260323" {
			t.Fatalf("unexpected query forwarding: %+v", fakeAPI.query)
		}
		if got, want := util.DerefZero(request.PhoneNumber), " 13800000000 "; got != want {
			t.Fatalf("original query phone = %q, want %q", got, want)
		}
	})
}

type testContextKey string

type fakeSMSAPI struct {
	sendSMS struct {
		phone    string
		template string
		ctxValue string
		runtime  *Option
	}
	batch struct {
		template string
	}
	query struct {
		phone string
		date  string
	}
}

func (f *fakeSMSAPI) SendSmsWithContext(ctx context.Context, request *SendSmsRequest, runtime *Option) (*SendSmsResponse, error) {
	f.sendSMS.phone = util.DerefZero(request.PhoneNumbers)
	f.sendSMS.template = util.DerefZero(request.TemplateCode)
	f.sendSMS.runtime = runtime
	if value, _ := ctx.Value(testContextKey("trace")).(string); value != "" {
		f.sendSMS.ctxValue = value
	}
	return &SendSmsResponse{}, nil
}

func (f *fakeSMSAPI) SendBatchSmsWithContext(_ context.Context, request *SendBatchSmsRequest, _ *Option) (*SendBatchSmsResponse, error) {
	f.batch.template = util.DerefZero(request.TemplateCode)
	return &SendBatchSmsResponse{}, nil
}

func (f *fakeSMSAPI) QuerySendDetailsWithContext(_ context.Context, request *QuerySendDetailsRequest, _ *Option) (*QuerySendDetailsResponse, error) {
	f.query.phone = util.DerefZero(request.PhoneNumber)
	f.query.date = util.DerefZero(request.SendDate)
	return &QuerySendDetailsResponse{}, nil
}
