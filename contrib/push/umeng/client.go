package umeng

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultEndpoint = "https://msgapi.umeng.com"

var (
	ErrNilConfig             = errors.New("umeng: config is required")
	ErrContextRequired       = errors.New("umeng: context is required")
	ErrRequestRequired       = errors.New("umeng: request is required")
	ErrAppKeyRequired        = errors.New("umeng: app key is required")
	ErrMasterSecretRequired  = errors.New("umeng: master secret is required")
	ErrInvalidEndpoint       = errors.New("umeng: endpoint is invalid")
	ErrPlatformRequired      = errors.New("umeng: platform is required")
	ErrNotificationBodyEmpty = errors.New("umeng: notification body is required")
	ErrAndroidTitleRequired  = errors.New("umeng: android notification title is required")
	ErrAliasTypeRequired     = errors.New("umeng: alias type is required when aliases are provided")
	ErrTargetRequired        = errors.New("umeng: exactly one target is required")
	ErrURLRequired           = errors.New("umeng: url is required when after_open is go_url")
	ErrActivityRequired      = errors.New("umeng: activity is required when after_open is go_activity")
	ErrCustomRequired        = errors.New("umeng: custom payload is required when after_open is go_custom")
)

type Platform string

const (
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
)

type AfterOpen string

const (
	AfterOpenGoApp      AfterOpen = "go_app"
	AfterOpenGoURL      AfterOpen = "go_url"
	AfterOpenGoActivity AfterOpen = "go_activity"
	AfterOpenGoCustom   AfterOpen = "go_custom"
)

type Config struct {
	AppKey       string
	MasterSecret string
	Endpoint     string
	AliasType    string
	Production   bool
	UserAgent    string
	HTTPClient   *http.Client

	now func() time.Time
}

type SendRequest struct {
	Platform     Platform
	Broadcast    bool
	Aliases      []string
	AliasType    string
	DeviceTokens []string
	Title        string
	Body         string
	Ticker       string
	Description  string
	AfterOpen    AfterOpen
	URL          string
	Activity     string
	Custom       string
	Extra        map[string]string
	Badge        *int
	Sound        string
	ExpireAfter  time.Duration
}

type SendResponse struct {
	Ret  string           `json:"ret"`
	Data SendResponseData `json:"data"`
}

type SendResponseData struct {
	MsgID     string `json:"msg_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	ErrorMsg  string `json:"error_msg,omitempty"`
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	base := fmt.Sprintf("umeng: request failed status=%d", e.StatusCode)
	if e.Code != "" {
		base += " code=" + e.Code
	}
	if e.Message != "" {
		base += " message=" + e.Message
	}
	return base
}

type Client interface {
	Raw() *http.Client
	Send(context.Context, *SendRequest) (*SendResponse, error)
}

type client struct {
	config     *Config
	httpClient *http.Client
}

func Open(cfg *Config) (Client, error) {
	return New(cfg)
}

func New(cfg *Config) (Client, error) {
	config, err := prepareConfig(cfg)
	if err != nil {
		return nil, err
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	return &client{
		config:     config,
		httpClient: httpClient,
	}, nil
}

func (c *client) Raw() *http.Client {
	return c.httpClient
}

func (c *client) Send(ctx context.Context, request *SendRequest) (*SendResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}

	prepared, err := prepareSendRequest(c.config, request)
	if err != nil {
		return nil, err
	}

	body, err := buildRequestBody(c.config, prepared)
	if err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(c.config.Endpoint)
	if err != nil {
		return nil, ErrInvalidEndpoint
	}
	endpoint.Path = "/api/send"

	sign := calculateSign(http.MethodPost, endpoint.String(), body, c.config.MasterSecret)
	query := endpoint.Query()
	query.Set("sign", sign)
	endpoint.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("umeng: build request failed: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if c.config.UserAgent != "" {
		httpRequest.Header.Set("User-Agent", c.config.UserAgent)
	}

	resp, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("umeng: send request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("umeng: read response failed: %w", err)
	}

	var result SendResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		if resp.StatusCode >= http.StatusBadRequest {
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Message:    strings.TrimSpace(string(respBody)),
			}
		}
		return nil, fmt.Errorf("umeng: decode response failed: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest || strings.EqualFold(result.Ret, "FAIL") {
		return &result, &APIError{
			StatusCode: resp.StatusCode,
			Code:       result.Data.ErrorCode,
			Message:    result.Data.ErrorMsg,
		}
	}

	return &result, nil
}

func prepareConfig(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	cloned := *cfg
	cloned.AppKey = strings.TrimSpace(cloned.AppKey)
	cloned.MasterSecret = strings.TrimSpace(cloned.MasterSecret)
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.AliasType = strings.TrimSpace(cloned.AliasType)
	cloned.UserAgent = strings.TrimSpace(cloned.UserAgent)

	switch {
	case cloned.AppKey == "":
		return nil, ErrAppKeyRequired
	case cloned.MasterSecret == "":
		return nil, ErrMasterSecretRequired
	}

	if cloned.Endpoint == "" {
		cloned.Endpoint = DefaultEndpoint
	}
	parsed, err := url.Parse(cloned.Endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, ErrInvalidEndpoint
	}
	if cloned.now == nil {
		cloned.now = time.Now
	}

	return &cloned, nil
}

func prepareSendRequest(cfg *Config, request *SendRequest) (*SendRequest, error) {
	if request == nil {
		return nil, ErrRequestRequired
	}

	cloned := *request
	cloned.Platform = Platform(strings.TrimSpace(string(cloned.Platform)))
	cloned.Title = strings.TrimSpace(cloned.Title)
	cloned.Body = strings.TrimSpace(cloned.Body)
	cloned.Ticker = strings.TrimSpace(cloned.Ticker)
	cloned.Description = strings.TrimSpace(cloned.Description)
	cloned.AliasType = strings.TrimSpace(cloned.AliasType)
	cloned.URL = strings.TrimSpace(cloned.URL)
	cloned.Activity = strings.TrimSpace(cloned.Activity)
	cloned.Custom = strings.TrimSpace(cloned.Custom)
	cloned.Sound = strings.TrimSpace(cloned.Sound)
	cloned.Aliases = trimStringSlice(cloned.Aliases)
	cloned.DeviceTokens = trimStringSlice(cloned.DeviceTokens)
	cloned.Extra = cloneStringMap(cloned.Extra)

	if cloned.Platform == "" {
		return nil, ErrPlatformRequired
	}
	if cloned.Body == "" {
		return nil, ErrNotificationBodyEmpty
	}
	if cloned.Platform == PlatformAndroid && cloned.Title == "" {
		return nil, ErrAndroidTitleRequired
	}
	if err := validateTarget(cfg, &cloned); err != nil {
		return nil, err
	}

	if cloned.AfterOpen == "" {
		cloned.AfterOpen = AfterOpenGoApp
	}
	switch cloned.AfterOpen {
	case AfterOpenGoURL:
		if cloned.URL == "" {
			return nil, ErrURLRequired
		}
	case AfterOpenGoActivity:
		if cloned.Activity == "" {
			return nil, ErrActivityRequired
		}
	case AfterOpenGoCustom:
		if cloned.Custom == "" {
			return nil, ErrCustomRequired
		}
	}
	if cloned.Ticker == "" {
		cloned.Ticker = firstNonEmpty(cloned.Title, cloned.Body)
	}
	if cloned.Description == "" {
		cloned.Description = cloned.Body
	}
	if cloned.Sound == "" && cloned.Platform == PlatformIOS {
		cloned.Sound = "default"
	}

	return &cloned, nil
}

func validateTarget(cfg *Config, request *SendRequest) error {
	targetCount := 0
	if request.Broadcast {
		targetCount++
	}
	if len(request.Aliases) > 0 {
		targetCount++
		if firstNonEmpty(request.AliasType, cfg.AliasType) == "" {
			return ErrAliasTypeRequired
		}
	}
	if len(request.DeviceTokens) > 0 {
		targetCount++
	}
	if targetCount != 1 {
		return ErrTargetRequired
	}
	return nil
}

func buildRequestBody(cfg *Config, request *SendRequest) ([]byte, error) {
	timestamp := strconv.FormatInt(cfg.now().Unix(), 10)
	castType, targetBody := buildTargetBody(cfg, request)
	payload, err := buildPayload(request)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"appkey":          cfg.AppKey,
		"timestamp":       timestamp,
		"type":            castType,
		"payload":         payload,
		"production_mode": strconv.FormatBool(cfg.Production),
		"description":     request.Description,
	}
	for key, value := range targetBody {
		body[key] = value
	}

	if request.ExpireAfter > 0 {
		body["policy"] = map[string]any{
			"expire_time": cfg.now().Add(request.ExpireAfter).UTC().Format("2006-01-02 15:04:05"),
		}
	}

	return json.Marshal(body)
}

func buildTargetBody(cfg *Config, request *SendRequest) (string, map[string]any) {
	if request.Broadcast {
		return "broadcast", map[string]any{}
	}
	if len(request.DeviceTokens) > 0 {
		if len(request.DeviceTokens) == 1 {
			return "unicast", map[string]any{"device_tokens": request.DeviceTokens[0]}
		}
		return "listcast", map[string]any{"device_tokens": strings.Join(request.DeviceTokens, ",")}
	}

	return "customizedcast", map[string]any{
		"alias":      strings.Join(request.Aliases, ","),
		"alias_type": firstNonEmpty(request.AliasType, cfg.AliasType),
	}
}

func buildPayload(request *SendRequest) (map[string]any, error) {
	switch request.Platform {
	case PlatformAndroid:
		return buildAndroidPayload(request), nil
	case PlatformIOS:
		return buildIOSPayload(request), nil
	default:
		return nil, ErrPlatformRequired
	}
}

func buildAndroidPayload(request *SendRequest) map[string]any {
	body := map[string]any{
		"ticker":     request.Ticker,
		"title":      request.Title,
		"text":       request.Body,
		"after_open": string(request.AfterOpen),
	}
	switch request.AfterOpen {
	case AfterOpenGoURL:
		body["url"] = request.URL
	case AfterOpenGoActivity:
		body["activity"] = request.Activity
	case AfterOpenGoCustom:
		body["custom"] = request.Custom
	}

	payload := map[string]any{
		"display_type": "notification",
		"body":         body,
	}
	if len(request.Extra) > 0 {
		payload["extra"] = request.Extra
	}
	return payload
}

func buildIOSPayload(request *SendRequest) map[string]any {
	aps := map[string]any{
		"alert": request.Body,
	}
	if request.Title != "" {
		aps["alert"] = map[string]any{
			"title": request.Title,
			"body":  request.Body,
		}
	}
	if request.Badge != nil {
		aps["badge"] = *request.Badge
	}
	if request.Sound != "" {
		aps["sound"] = request.Sound
	}

	payload := map[string]any{
		"aps": aps,
	}
	if len(request.Extra) > 0 {
		payload["extra"] = request.Extra
	}
	return payload
}

func calculateSign(method, rawURL string, body []byte, secret string) string {
	sum := md5.Sum([]byte(method + rawURL + string(body) + secret))
	return hex.EncodeToString(sum[:])
}

func trimStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = strings.TrimSpace(value)
	}
	return dst
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
