package sms

import (
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

func New(config *Config) (Client, error) {
	var err error
	client := &ClientEntity{}
	client.smsClient, err = dysmsapi.NewClient(config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (s *ClientEntity) SendSms(request *SendSmsRequest) (*SendSmsResponse, error) {
	return s.smsClient.SendSms(request)
}
func (s *ClientEntity) SendBatchSms(request *SendBatchSmsRequest) (*SendBatchSmsResponse, error) {
	return s.smsClient.SendBatchSms(request)
}
func (s *ClientEntity) QuerySendDetails(request *QuerySendDetailsRequest) (*QuerySendDetailsResponse, error) {
	return s.smsClient.QuerySendDetails(request)
}

func (s *ClientEntity) SendSmsWithOptions(request *SendSmsRequest, runtime *Option) (*SendSmsResponse, error) {
	return s.smsClient.SendSmsWithOptions(request, runtime)
}

func (s *ClientEntity) SendBatchSmsWithOptions(request *SendBatchSmsRequest, runtime *Option) (*SendBatchSmsResponse, error) {
	return s.smsClient.SendBatchSmsWithOptions(request, runtime)
}

func (s *ClientEntity) QuerySendDetailsWithOptions(request *QuerySendDetailsRequest, runtime *Option) (*QuerySendDetailsResponse, error) {
	return s.smsClient.QuerySendDetailsWithOptions(request, runtime)
}
