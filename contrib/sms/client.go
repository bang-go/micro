package sms

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/models"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v5/client"
	teaUtil "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/bang-go/util"
)

var (
	ErrNilConfig               = errors.New("sms: config is required")
	ErrContextRequired         = errors.New("sms: context is required")
	ErrAccessKeyIDRequired     = errors.New("sms: access key id is required")
	ErrAccessKeySecretRequired = errors.New("sms: access key secret is required")
	ErrSendRequestRequired     = errors.New("sms: send sms request is required")
	ErrBatchRequestRequired    = errors.New("sms: send batch sms request is required")
	ErrQueryRequestRequired    = errors.New("sms: query send details request is required")
	ErrPhoneNumbersRequired    = errors.New("sms: phone numbers are required")
	ErrSignNameRequired        = errors.New("sms: sign name is required")
	ErrTemplateCodeRequired    = errors.New("sms: template code is required")
	ErrPhoneNumberRequired     = errors.New("sms: phone number is required")
	ErrSendDateRequired        = errors.New("sms: send date is required")
)

type Config struct {
	AccessKeyID     string
	AccessKeySecret string
	Endpoint        string
	RegionID        string
	SecurityToken   string
	UserAgent       string

	newClient func(*openapi.Config) (smsAPI, error)
}

type Option = teaUtil.RuntimeOptions
type SendSmsRequest = dysmsapi.SendSmsRequest
type SendBatchSmsRequest = dysmsapi.SendBatchSmsRequest
type SendSmsResponse = dysmsapi.SendSmsResponse
type SendBatchSmsResponse = dysmsapi.SendBatchSmsResponse
type QuerySendDetailsRequest = dysmsapi.QuerySendDetailsRequest
type QuerySendDetailsResponse = dysmsapi.QuerySendDetailsResponse

type Client interface {
	Raw() *dysmsapi.Client
	SendSms(context.Context, *SendSmsRequest) (*SendSmsResponse, error)
	SendSmsWithOptions(context.Context, *SendSmsRequest, *Option) (*SendSmsResponse, error)
	SendBatchSms(context.Context, *SendBatchSmsRequest) (*SendBatchSmsResponse, error)
	SendBatchSmsWithOptions(context.Context, *SendBatchSmsRequest, *Option) (*SendBatchSmsResponse, error)
	QuerySendDetails(context.Context, *QuerySendDetailsRequest) (*QuerySendDetailsResponse, error)
	QuerySendDetailsWithOptions(context.Context, *QuerySendDetailsRequest, *Option) (*QuerySendDetailsResponse, error)
}

type smsAPI interface {
	SendSmsWithContext(context.Context, *SendSmsRequest, *Option) (*SendSmsResponse, error)
	SendBatchSmsWithContext(context.Context, *SendBatchSmsRequest, *Option) (*SendBatchSmsResponse, error)
	QuerySendDetailsWithContext(context.Context, *QuerySendDetailsRequest, *Option) (*QuerySendDetailsResponse, error)
}

type client struct {
	config *Config
	api    smsAPI
	raw    *dysmsapi.Client
}

func Open(conf *Config) (Client, error) {
	return New(conf)
}

func New(conf *Config) (Client, error) {
	config, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	api, err := config.newClient(buildOpenAPIConfig(config))
	if err != nil {
		return nil, fmt.Errorf("sms: create client failed: %w", err)
	}

	raw, _ := api.(*dysmsapi.Client)
	return &client{
		config: config,
		api:    api,
		raw:    raw,
	}, nil
}

func (c *client) Raw() *dysmsapi.Client {
	return c.raw
}

func (c *client) SendSms(ctx context.Context, request *SendSmsRequest) (*SendSmsResponse, error) {
	return c.SendSmsWithOptions(ctx, request, nil)
}

func (c *client) SendSmsWithOptions(ctx context.Context, request *SendSmsRequest, runtime *Option) (*SendSmsResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	prepared, err := prepareSendSMSRequest(request)
	if err != nil {
		return nil, err
	}
	return c.api.SendSmsWithContext(ctx, prepared, runtime)
}

func (c *client) SendBatchSms(ctx context.Context, request *SendBatchSmsRequest) (*SendBatchSmsResponse, error) {
	return c.SendBatchSmsWithOptions(ctx, request, nil)
}

func (c *client) SendBatchSmsWithOptions(ctx context.Context, request *SendBatchSmsRequest, runtime *Option) (*SendBatchSmsResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	prepared, err := prepareBatchRequest(request)
	if err != nil {
		return nil, err
	}
	return c.api.SendBatchSmsWithContext(ctx, prepared, runtime)
}

func (c *client) QuerySendDetails(ctx context.Context, request *QuerySendDetailsRequest) (*QuerySendDetailsResponse, error) {
	return c.QuerySendDetailsWithOptions(ctx, request, nil)
}

func (c *client) QuerySendDetailsWithOptions(ctx context.Context, request *QuerySendDetailsRequest, runtime *Option) (*QuerySendDetailsResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	prepared, err := prepareQueryRequest(request)
	if err != nil {
		return nil, err
	}
	return c.api.QuerySendDetailsWithContext(ctx, prepared, runtime)
}

func prepareConfig(conf *Config) (*Config, error) {
	if conf == nil {
		return nil, ErrNilConfig
	}

	cloned := *conf
	cloned.AccessKeyID = strings.TrimSpace(cloned.AccessKeyID)
	cloned.AccessKeySecret = strings.TrimSpace(cloned.AccessKeySecret)
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.RegionID = strings.TrimSpace(cloned.RegionID)
	cloned.SecurityToken = strings.TrimSpace(cloned.SecurityToken)
	cloned.UserAgent = strings.TrimSpace(cloned.UserAgent)

	switch {
	case cloned.AccessKeyID == "":
		return nil, ErrAccessKeyIDRequired
	case cloned.AccessKeySecret == "":
		return nil, ErrAccessKeySecretRequired
	}

	if cloned.newClient == nil {
		cloned.newClient = func(cfg *openapi.Config) (smsAPI, error) {
			return dysmsapi.NewClient(cfg)
		}
	}

	return &cloned, nil
}

func buildOpenAPIConfig(conf *Config) *openapi.Config {
	cfg := &openapi.Config{
		AccessKeyId:     util.Ptr(conf.AccessKeyID),
		AccessKeySecret: util.Ptr(conf.AccessKeySecret),
	}
	if conf.Endpoint != "" {
		cfg.Endpoint = util.Ptr(conf.Endpoint)
	}
	if conf.RegionID != "" {
		cfg.RegionId = util.Ptr(conf.RegionID)
	}
	if conf.SecurityToken != "" {
		cfg.SecurityToken = util.Ptr(conf.SecurityToken)
	}
	if conf.UserAgent != "" {
		cfg.UserAgent = util.Ptr(conf.UserAgent)
	}
	return cfg
}

func validateSendSMSRequest(request *SendSmsRequest) error {
	if request == nil {
		return ErrSendRequestRequired
	}
	if trimStringPointer(request.PhoneNumbers) == "" {
		return ErrPhoneNumbersRequired
	}
	if trimStringPointer(request.SignName) == "" {
		return ErrSignNameRequired
	}
	if trimStringPointer(request.TemplateCode) == "" {
		return ErrTemplateCodeRequired
	}
	return nil
}

func prepareSendSMSRequest(request *SendSmsRequest) (*SendSmsRequest, error) {
	if err := validateSendSMSRequest(request); err != nil {
		return nil, err
	}
	cloned := *request
	cloned.PhoneNumbers = normalizeStringPointer(request.PhoneNumbers)
	cloned.SignName = normalizeStringPointer(request.SignName)
	cloned.TemplateCode = normalizeStringPointer(request.TemplateCode)
	return &cloned, nil
}

func validateBatchRequest(request *SendBatchSmsRequest) error {
	if request == nil {
		return ErrBatchRequestRequired
	}
	if trimStringPointer(request.PhoneNumberJson) == "" {
		return ErrPhoneNumbersRequired
	}
	if trimStringPointer(request.SignNameJson) == "" {
		return ErrSignNameRequired
	}
	if trimStringPointer(request.TemplateCode) == "" {
		return ErrTemplateCodeRequired
	}
	return nil
}

func prepareBatchRequest(request *SendBatchSmsRequest) (*SendBatchSmsRequest, error) {
	if err := validateBatchRequest(request); err != nil {
		return nil, err
	}
	cloned := *request
	cloned.PhoneNumberJson = normalizeStringPointer(request.PhoneNumberJson)
	cloned.SignNameJson = normalizeStringPointer(request.SignNameJson)
	cloned.TemplateCode = normalizeStringPointer(request.TemplateCode)
	return &cloned, nil
}

func validateQueryRequest(request *QuerySendDetailsRequest) error {
	if request == nil {
		return ErrQueryRequestRequired
	}
	if trimStringPointer(request.PhoneNumber) == "" {
		return ErrPhoneNumberRequired
	}
	if trimStringPointer(request.SendDate) == "" {
		return ErrSendDateRequired
	}
	return nil
}

func prepareQueryRequest(request *QuerySendDetailsRequest) (*QuerySendDetailsRequest, error) {
	if err := validateQueryRequest(request); err != nil {
		return nil, err
	}
	cloned := *request
	cloned.PhoneNumber = normalizeStringPointer(request.PhoneNumber)
	cloned.SendDate = normalizeStringPointer(request.SendDate)
	return &cloned, nil
}

func trimStringPointer(value *string) string {
	return strings.TrimSpace(util.DerefZero(value))
}

func normalizeStringPointer(value *string) *string {
	trimmed := trimStringPointer(value)
	if trimmed == "" {
		return nil
	}
	return util.Ptr(trimmed)
}
