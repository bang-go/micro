package search

import (
	opensearchutil "github.com/alibabacloud-go/opensearch-util/service"
	util "github.com/alibabacloud-go/tea-utils/service"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
)

type Config struct {
	Endpoint        *string `json:"endpoint,omitempty" xml:"endpoint,omitempty"`
	Protocol        *string `json:"protocol,omitempty" xml:"protocol,omitempty"`
	Type            *string `json:"type,omitempty" xml:"type,omitempty"`
	SecurityToken   *string `json:"securityToken,omitempty" xml:"securityToken,omitempty"`
	AccessKeyId     *string `json:"accessKeyId,omitempty" xml:"accessKeyId,omitempty"`
	AccessKeySecret *string `json:"accessKeySecret,omitempty" xml:"accessKeySecret,omitempty"`
	UserAgent       *string `json:"userAgent,omitempty" xml:"userAgent,omitempty"`
}

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type ResponseHeader struct {
	Connection     string `json:"connection"`
	ContentType    string `json:"content-type"`
	Date           string `json:"date"`
	KeepAlive      string `json:"keep-alive"`
	RequestId      string `json:"request-id"`
	Server         string `json:"server"`
	XAcsApiName    string `json:"x-acs-api-name"`
	XAcsCallerType string `json:"x-acs-caller-type"`
	XAcsCallerUid  string `json:"x-acs-caller-uid"`
	XAcsParentUid  string `json:"x-acs-parent-uid"`
	XAliyunUserId  string `json:"x-aliyun-user-id"`
	XAppId         string `json:"x-app-id"`
	XAppName       string `json:"x-app-name"`
	XAppgroupId    string `json:"x-appgroup-id"`
}
type ResponseSuggestList struct {
	Headers ResponseHeader `json:"headers"`
	Body    SuggestBody    `json:"body"`
}
type SuggestBody struct {
	RequestId   string        `json:"request_id"`
	SearchTime  float64       `json:"searchtime"`
	Suggestions []SuggestItem `json:"suggestions"`
}
type SuggestItem struct {
	Suggestion string `json:"suggestion"`
}

type ResponseSearchList[T any] struct {
	Headers ResponseHeader `json:"headers"`
	Body    SearchBody[T]  `json:"body"`
}
type SearchBody[T any] struct {
	Error          []ResponseError `json:"error"`
	OpsRequestMisc string          `json:"ops_request_misc"`
	RequestId      string          `json:"request_id"`
	Status         string          `json:"status"`
	Trace          string          `json:"trace"`
	Result         struct {
		Facet      []any               `json:"facet"`
		Num        uint32              `json:"num"`
		SearchTime float64             `json:"searchtime"`
		Total      uint32              `json:"total"`
		ViewTotal  uint32              `json:"viewtotal"`
		Items      []SearchBodyItem[T] `json:"Items"`
	} `json:"result"`
}

type SearchBodyItem[T any] struct {
	VariableValue  map[string]any `json:"variableValue"`
	Property       map[string]any `json:"property"`
	Attribute      map[string]any `json:"attribute"`
	SortExprValues []any          `json:"sortExprValues"`
	Fields         T              `json:"fields"`
}

type Client struct {
	Endpoint   *string
	Protocol   *string
	UserAgent  *string
	Credential credential.Credential
}

func NewClient(config *Config) (*Client, error) {
	client := new(Client)
	err := client.Init(config)
	return client, err
}

func (client *Client) Init(config *Config) (_err error) {
	if tea.BoolValue(util.IsUnset(tea.ToMap(config))) {
		_err = tea.NewSDKError(map[string]interface{}{
			"name":    "ParameterMissing",
			"message": "'config' can not be unset",
		})
		return _err
	}

	if tea.BoolValue(util.Empty(config.Type)) {
		config.Type = tea.String("access_key")
	}

	credentialConfig := &credential.Config{
		AccessKeyId:     config.AccessKeyId,
		Type:            config.Type,
		AccessKeySecret: config.AccessKeySecret,
		SecurityToken:   config.SecurityToken,
	}
	client.Credential, _err = credential.NewCredential(credentialConfig)
	if _err != nil {
		return _err
	}

	client.Endpoint = config.Endpoint
	client.Protocol = config.Protocol
	client.UserAgent = config.UserAgent
	return nil
}

func (client *Client) Request(method *string, pathname *string, query map[string]interface{}, headers map[string]*string, body interface{}, runtime *util.RuntimeOptions) (_result map[string]interface{}, _err error) {
	_err = tea.Validate(runtime)
	if _err != nil {
		return _result, _err
	}
	_runtime := map[string]interface{}{
		"timeouted":      "retry",
		"readTimeout":    tea.IntValue(runtime.ReadTimeout),
		"connectTimeout": tea.IntValue(runtime.ConnectTimeout),
		"httpProxy":      tea.StringValue(runtime.HttpProxy),
		"httpsProxy":     tea.StringValue(runtime.HttpsProxy),
		"noProxy":        tea.StringValue(runtime.NoProxy),
		"maxIdleConns":   tea.IntValue(runtime.MaxIdleConns),
		"retry": map[string]interface{}{
			"retryable":   tea.BoolValue(runtime.Autoretry),
			"maxAttempts": tea.IntValue(util.DefaultNumber(runtime.MaxAttempts, tea.Int(3))),
		},
		"backoff": map[string]interface{}{
			"policy": tea.StringValue(util.DefaultString(runtime.BackoffPolicy, tea.String("no"))),
			"period": tea.IntValue(util.DefaultNumber(runtime.BackoffPeriod, tea.Int(1))),
		},
		"ignoreSSL": tea.BoolValue(runtime.IgnoreSSL),
	}

	_resp := make(map[string]interface{})
	for _retryTimes := 0; tea.BoolValue(tea.AllowRetry(_runtime["retry"], tea.Int(_retryTimes))); _retryTimes++ {
		if _retryTimes > 0 {
			_backoffTime := tea.GetBackoffTime(_runtime["backoff"], tea.Int(_retryTimes))
			if tea.IntValue(_backoffTime) > 0 {
				tea.Sleep(_backoffTime)
			}
		}

		_resp, _err = func() (map[string]interface{}, error) {
			request_ := tea.NewRequest()
			accessKeyId, _err := client.GetAccessKeyId()
			if _err != nil {
				return _result, _err
			}

			accessKeySecret, _err := client.GetAccessKeySecret()
			if _err != nil {
				return _result, _err
			}

			request_.Protocol = util.DefaultString(client.Protocol, tea.String("HTTP"))
			request_.Method = method
			request_.Pathname = pathname
			request_.Headers = tea.Merge(map[string]*string{
				"user-agent":         client.GetUserAgent(),
				"Date":               opensearchutil.GetDate(),
				"host":               util.DefaultString(client.Endpoint, tea.String("opensearch-cn-hangzhou.aliyuncs.com")),
				"X-Opensearch-Nonce": util.GetNonce(),
			}, headers)
			if !tea.BoolValue(util.IsUnset(query)) {
				request_.Query = util.StringifyMapValue(query)
			}

			if !tea.BoolValue(util.IsUnset(body)) {
				reqBody := util.ToJSONString(body)
				request_.Headers["Content-MD5"] = opensearchutil.GetContentMD5(reqBody)
				request_.Headers["Content-Type"] = tea.String("application/json")
				request_.Body = tea.ToReader(reqBody)
			}

			request_.Headers["Authorization"] = opensearchutil.GetSignature(request_, accessKeyId, accessKeySecret)
			response_, _err := tea.DoRequest(request_, _runtime)
			if _err != nil {
				return _result, _err
			}
			objStr, _err := util.ReadAsString(response_.Body)
			if _err != nil {
				return _result, _err
			}

			if tea.BoolValue(util.Is4xx(response_.StatusCode)) || tea.BoolValue(util.Is5xx(response_.StatusCode)) {
				_err = tea.NewSDKError(map[string]interface{}{
					"message": tea.StringValue(response_.StatusMessage),
					"data":    tea.StringValue(objStr),
					"code":    tea.IntValue(response_.StatusCode),
				})
				return _result, _err
			}

			obj := util.ParseJSON(objStr)
			res := util.AssertAsMap(obj)
			_result = make(map[string]interface{})
			_err = tea.Convert(map[string]interface{}{
				"body":    res,
				"headers": response_.Headers,
			}, &_result)
			return _result, _err
		}()
		if !tea.BoolValue(tea.Retryable(_err)) {
			break
		}
	}

	return _resp, _err
}

func (client *Client) SetUserAgent(userAgent *string) {
	client.UserAgent = userAgent
}

func (client *Client) AppendUserAgent(userAgent *string) {
	client.UserAgent = tea.String(tea.StringValue(client.UserAgent) + " " + tea.StringValue(userAgent))
}

func (client *Client) GetUserAgent() (_result *string) {
	userAgent := util.GetUserAgent(client.UserAgent)
	_result = userAgent
	return _result
}

func (client *Client) GetAccessKeyId() (_result *string, _err error) {
	if tea.BoolValue(util.IsUnset(client.Credential)) {
		return _result, _err
	}

	cred, _err := client.Credential.GetCredential()
	if _err != nil {
		return _result, _err
	}

	_result = cred.AccessKeyId
	return _result, _err
}

func (client *Client) GetAccessKeySecret() (_result *string, _err error) {
	if tea.BoolValue(util.IsUnset(client.Credential)) {
		return _result, _err
	}

	cred, _err := client.Credential.GetCredential()
	if _err != nil {
		return _result, _err
	}

	_result = cred.AccessKeySecret
	return _result, _err
}
