package sms

import (
	"fmt"

	"github.com/alibabacloud-go/darabonba-openapi/v2/models"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v5/client"
	teeUtil "github.com/alibabacloud-go/tea-utils/v2/service"
)

type Config = models.Config
type Option = teeUtil.RuntimeOptions
type SendSmsRequest = dysmsapi.SendSmsRequest
type SendBatchSmsRequest = dysmsapi.SendBatchSmsRequest
type SendSmsResponse = dysmsapi.SendSmsResponse
type SendBatchSmsResponse = dysmsapi.SendBatchSmsResponse
type QuerySendDetailsRequest = dysmsapi.QuerySendDetailsRequest
type QuerySendDetailsResponse = dysmsapi.QuerySendDetailsResponse
type Client interface {
	SendSms(*SendSmsRequest) (*SendSmsResponse, error)
	SendSmsWithOptions(*SendSmsRequest, *Option) (*SendSmsResponse, error)
	SendBatchSms(*SendBatchSmsRequest) (*SendBatchSmsResponse, error)
	SendBatchSmsWithOptions(*SendBatchSmsRequest, *Option) (*SendBatchSmsResponse, error)
	QuerySendDetails(*QuerySendDetailsRequest) (*QuerySendDetailsResponse, error)
	QuerySendDetailsWithOptions(*QuerySendDetailsRequest, *Option) (*QuerySendDetailsResponse, error)
}

type ClientEntity struct {
	*Config
	smsClient *dysmsapi.Client
}

// New 创建新的短信服务客户端
// config: 短信服务配置
// 返回: Client 实例和错误
func New(config *Config) (Client, error) {
	if config == nil {
		return nil, fmt.Errorf("Config 不能为 nil")
	}

	client := &ClientEntity{Config: config}
	var err error
	client.smsClient, err = dysmsapi.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建短信服务客户端失败: %w", err)
	}
	return client, nil
}

// SendSms 发送短信
func (s *ClientEntity) SendSms(request *SendSmsRequest) (*SendSmsResponse, error) {
	return s.smsClient.SendSms(request)
}

// SendBatchSms 批量发送短信
func (s *ClientEntity) SendBatchSms(request *SendBatchSmsRequest) (*SendBatchSmsResponse, error) {
	return s.smsClient.SendBatchSms(request)
}

// QuerySendDetails 查询短信发送详情
func (s *ClientEntity) QuerySendDetails(request *QuerySendDetailsRequest) (*QuerySendDetailsResponse, error) {
	return s.smsClient.QuerySendDetails(request)
}

// SendSmsWithOptions 发送短信（带运行时选项）
func (s *ClientEntity) SendSmsWithOptions(request *SendSmsRequest, runtime *Option) (*SendSmsResponse, error) {
	return s.smsClient.SendSmsWithOptions(request, runtime)
}

// SendBatchSmsWithOptions 批量发送短信（带运行时选项）
func (s *ClientEntity) SendBatchSmsWithOptions(request *SendBatchSmsRequest, runtime *Option) (*SendBatchSmsResponse, error) {
	return s.smsClient.SendBatchSmsWithOptions(request, runtime)
}

// QuerySendDetailsWithOptions 查询短信发送详情（带运行时选项）
func (s *ClientEntity) QuerySendDetailsWithOptions(request *QuerySendDetailsRequest, runtime *Option) (*QuerySendDetailsResponse, error) {
	return s.smsClient.QuerySendDetailsWithOptions(request, runtime)
}
