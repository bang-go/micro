package opensearchx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	opensearchutil "github.com/alibabacloud-go/opensearch-util/service"
	teaUtil "github.com/alibabacloud-go/tea-utils/service"
	"github.com/alibabacloud-go/tea/tea"
)

const (
	defaultProtocol       = "https"
	defaultConnectTimeout = 5 * time.Second
	defaultReadTimeout    = 10 * time.Second
	defaultMaxIdleConns   = 50
	defaultSearchHit      = 10
	defaultSearchFormat   = "fulljson"
)

var (
	ErrNilConfig             = errors.New("opensearchx: config is required")
	ErrContextRequired       = errors.New("opensearchx: context is required")
	ErrEndpointRequired      = errors.New("opensearchx: endpoint is required")
	ErrAccessKeyIDRequired   = errors.New("opensearchx: access key id is required")
	ErrAccessKeySecretNeeded = errors.New("opensearchx: access key secret is required")
	ErrInvalidProtocol       = errors.New("opensearchx: protocol must be http or https")
	ErrAppNameRequired       = errors.New("opensearchx: app name is required")
	ErrModelNameRequired     = errors.New("opensearchx: model name is required")
	ErrQueryRequired         = errors.New("opensearchx: query is required")
	ErrSearchRequestRequired = errors.New("opensearchx: search request is required")
	ErrHintRequestRequired   = errors.New("opensearchx: hint request is required")
	ErrHotRequestRequired    = errors.New("opensearchx: hot search request is required")
	ErrSuggestRequestNeeded  = errors.New("opensearchx: suggest request is required")
	ErrRequestMethodRequired = errors.New("opensearchx: request method is required")
	ErrRequestPathRequired   = errors.New("opensearchx: request path is required")
)

type Config struct {
	Endpoint        string
	Protocol        string
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	UserAgent       string
	ConnectTimeout  time.Duration
	ReadTimeout     time.Duration
	MaxIdleConns    int

	newRequester func(*Config) requester
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

type SuggestResponse struct {
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

type HintResponse struct {
	Headers ResponseHeader `json:"headers"`
	Body    HintBody       `json:"body"`
}

type HintBody struct {
	RequestId  string     `json:"request_id"`
	SearchTime float64    `json:"searchtime"`
	Result     []HintItem `json:"result"`
}

type HintItem struct {
	Hint string `json:"hint"`
	Pv   *int64 `json:"pv,omitempty"`
}

type HotSearchResponse struct {
	Headers ResponseHeader `json:"headers"`
	Body    HotSearchBody  `json:"body"`
}

type HotSearchBody struct {
	RequestId  string          `json:"request_id"`
	SearchTime float64         `json:"searchtime"`
	Result     []HotSearchItem `json:"result"`
}

type HotSearchItem struct {
	Hot  string `json:"hot"`
	Pv   *int64 `json:"pv,omitempty"`
	Tags *int   `json:"tags,omitempty"`
}

type SearchResponse[T any] struct {
	Headers ResponseHeader `json:"headers"`
	Body    SearchBody[T]  `json:"body"`
}

type SearchBody[T any] struct {
	Error          []ResponseError `json:"error"`
	OpsRequestMisc string          `json:"ops_request_misc"`
	RequestId      string          `json:"request_id"`
	Status         string          `json:"status"`
	Trace          string          `json:"trace"`
	Result         SearchResult[T] `json:"result"`
}

type SearchResult[T any] struct {
	ComputeCost []any   `json:"compute_cost"`
	Facet       []any   `json:"facet"`
	Num         uint32  `json:"num"`
	SearchTime  float64 `json:"searchtime"`
	Total       uint32  `json:"total"`
	ViewTotal   uint32  `json:"viewtotal"`
	Items       []T     `json:"items"`
}

type QueryClause struct {
	Index      string
	Value      string
	ExactMatch bool
}

func (q *QueryClause) String() string {
	if q == nil {
		return ""
	}
	index := strings.TrimSpace(q.Index)
	value := strings.TrimSpace(q.Value)
	if index == "" || value == "" {
		return ""
	}
	if !isQuoted(value) && (q.ExactMatch || !strings.ContainsAny(value, " :'\"")) {
		value = "'" + value + "'"
	}
	return fmt.Sprintf("%s:%s", index, value)
}

type FilterClause struct {
	Field    string
	Operator string
	Value    any
}

func (f *FilterClause) String() string {
	if f == nil {
		return ""
	}
	field := strings.TrimSpace(f.Field)
	operator := strings.TrimSpace(f.Operator)
	if field == "" || operator == "" || f.Value == nil {
		return ""
	}
	return field + operator + formatFilterValue(f.Value)
}

type SortClause struct {
	Field string
	Order string
}

func (s *SortClause) String() string {
	if s == nil {
		return ""
	}
	field := strings.TrimSpace(s.Field)
	if field == "" {
		return ""
	}
	order := strings.TrimSpace(s.Order)
	if order == "" {
		order = "-"
	}
	return field + ":" + order
}

type DistinctClause struct {
	Key   string
	Count int
	Times int
}

func (d *DistinctClause) String() string {
	if d == nil {
		return ""
	}
	key := strings.TrimSpace(d.Key)
	if key == "" {
		return ""
	}
	parts := []string{"dist_key:" + key}
	if d.Count > 0 {
		parts = append(parts, fmt.Sprintf("dist_count:%d", d.Count))
	}
	if d.Times > 0 {
		parts = append(parts, fmt.Sprintf("dist_times:%d", d.Times))
	}
	return strings.Join(parts, ",")
}

type AggregateClause struct {
	GroupKey string
	AggFun   string
}

func (a *AggregateClause) String() string {
	if a == nil {
		return ""
	}
	groupKey := strings.TrimSpace(a.GroupKey)
	aggFun := strings.TrimSpace(a.AggFun)
	if groupKey == "" || aggFun == "" {
		return ""
	}
	return "group_key:" + groupKey + ",agg_fun:" + aggFun
}

type SearchRequest struct {
	Query           *QueryClause
	Filter          *FilterClause
	Sort            *SortClause
	Distinct        *DistinctClause
	Aggregate       *AggregateClause
	KVPairs         string
	Start           int
	Hit             int
	Format          string
	Config          string
	FetchFields     string
	QP              string
	Disable         string
	FirstRankName   string
	SecondRankName  string
	UserID          string
	ABTest          string
	RawQuery        string
	SearchStrategy  string
	ReSearch        string
	Biz             string
	Summary         *SummaryConfig
	FromRequestID   string
	VectorThreshold *float64
}

type SummaryConfig struct {
	SummaryField          string
	SummaryElement        string
	SummaryEllipsis       string
	SummarySnipped        int
	SummaryLen            string
	SummaryElementPrefix  string
	SummaryElementPostfix string
}

func (s *SummaryConfig) String() string {
	if s == nil {
		return ""
	}
	field := strings.TrimSpace(s.SummaryField)
	if field == "" {
		return ""
	}
	parts := []string{"summary_field:" + field}
	appendQueryPart(&parts, "summary_element", s.SummaryElement)
	appendQueryPart(&parts, "summary_ellipsis", s.SummaryEllipsis)
	if s.SummarySnipped > 0 {
		parts = append(parts, fmt.Sprintf("summary_snipped:%d", s.SummarySnipped))
	}
	appendQueryPart(&parts, "summary_len", s.SummaryLen)
	appendQueryPart(&parts, "summary_element_prefix", s.SummaryElementPrefix)
	appendQueryPart(&parts, "summary_element_postfix", s.SummaryElementPostfix)
	return strings.Join(parts, ",")
}

func (r *SearchRequest) String() string {
	if r == nil || r.Query == nil {
		return ""
	}
	query := r.Query.String()
	if query == "" {
		return ""
	}

	start := r.Start
	if start < 0 {
		start = 0
	}
	hit := r.Hit
	if hit <= 0 {
		hit = defaultSearchHit
	}
	format := strings.TrimSpace(r.Format)
	if format == "" {
		format = defaultSearchFormat
	}

	configParts := []string{
		fmt.Sprintf("start:%d", start),
		fmt.Sprintf("hit:%d", hit),
		fmt.Sprintf("format:%s", format),
	}
	if extra := strings.TrimSpace(r.Config); extra != "" {
		configParts = append(configParts, extra)
	}

	parts := []string{
		"query=" + query,
		"config=" + strings.Join(configParts, ","),
	}

	appendClause(&parts, "filter", r.Filter.String())
	appendClause(&parts, "sort", r.Sort.String())
	appendClause(&parts, "distinct", r.Distinct.String())
	appendClause(&parts, "aggregate", r.Aggregate.String())
	appendClause(&parts, "kvpairs", r.KVPairs)
	appendClause(&parts, "fetch_fields", r.FetchFields)
	appendClause(&parts, "qp", r.QP)
	appendClause(&parts, "disable", r.Disable)
	appendClause(&parts, "first_rank_name", r.FirstRankName)
	appendClause(&parts, "second_rank_name", r.SecondRankName)
	appendClause(&parts, "user_id", r.UserID)
	appendClause(&parts, "abtest", r.ABTest)
	appendClause(&parts, "raw_query", r.RawQuery)
	appendClause(&parts, "search_strategy", r.SearchStrategy)
	appendClause(&parts, "re_search", r.ReSearch)
	appendClause(&parts, "biz", r.Biz)
	appendClause(&parts, "summary", r.Summary.String())
	appendClause(&parts, "from_request_id", r.FromRequestID)
	if r.VectorThreshold != nil {
		appendClause(&parts, "vector_threshold", fmt.Sprintf("%v", *r.VectorThreshold))
	}

	return strings.Join(parts, "&&")
}

type HintRequest struct {
	Hit       int
	SortType  string
	UserID    string
	ModelName string
}

func (r *HintRequest) ToQuery() map[string]string {
	values := make(map[string]string)
	if r == nil {
		return values
	}
	appendMap(values, "hit", positiveIntString(r.Hit))
	appendMap(values, "sort_type", r.SortType)
	appendMap(values, "user_id", r.UserID)
	appendMap(values, "model_name", r.ModelName)
	return values
}

type HotSearchRequest struct {
	Hit       int
	SortType  string
	UserID    string
	ModelName string
}

func (r *HotSearchRequest) ToQuery() map[string]string {
	values := make(map[string]string)
	if r == nil {
		return values
	}
	appendMap(values, "hit", positiveIntString(r.Hit))
	appendMap(values, "sort_type", r.SortType)
	appendMap(values, "user_id", r.UserID)
	appendMap(values, "model_name", r.ModelName)
	return values
}

type SuggestRequest struct {
	Query  string
	Hit    int
	UserID string
}

func (r *SuggestRequest) ToQuery() map[string]string {
	values := make(map[string]string)
	if r == nil {
		return values
	}
	appendMap(values, "query", r.Query)
	appendMap(values, "hit", positiveIntString(r.Hit))
	appendMap(values, "user_id", r.UserID)
	return values
}

type Client interface {
	Search(context.Context, string, *SearchRequest) (map[string]any, error)
	Suggest(context.Context, string, string, *SuggestRequest) (*SuggestResponse, error)
	Hint(context.Context, string, *HintRequest) (*HintResponse, error)
	HotSearch(context.Context, string, *HotSearchRequest) (*HotSearchResponse, error)
	Request(context.Context, string, string, map[string]string, map[string]string, any) (map[string]any, error)
}

type client struct {
	config    *Config
	requester requester
}

type requester interface {
	Do(context.Context, requestSpec) (map[string]any, error)
}

type requestSpec struct {
	Method  string
	Path    string
	Query   map[string]string
	Headers map[string]string
	Body    any
}

func Open(conf *Config) (Client, error) {
	return New(conf)
}

func New(conf *Config) (Client, error) {
	config, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	return &client{
		config:    config,
		requester: config.newRequester(config),
	}, nil
}

func (c *client) Search(ctx context.Context, appName string, req *SearchRequest) (map[string]any, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, ErrAppNameRequired
	}
	if req == nil {
		return nil, ErrSearchRequestRequired
	}
	query := req.String()
	if query == "" {
		return nil, ErrQueryRequired
	}

	return c.Request(ctx, "GET", fmt.Sprintf("/v3/openapi/apps/%s/search", appName), map[string]string{
		"query": query,
	}, nil, nil)
}

func SearchTyped[T any](client Client, ctx context.Context, appName string, req *SearchRequest) (*SearchResponse[T], error) {
	result, err := client.Search(ctx, appName, req)
	if err != nil {
		return nil, err
	}

	var response SearchResponse[T]
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("opensearchx: parse response failed: %w", err)
	}
	return &response, nil
}

func (c *client) Suggest(ctx context.Context, appName, modelName string, req *SuggestRequest) (*SuggestResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	appName = strings.TrimSpace(appName)
	modelName = strings.TrimSpace(modelName)
	if appName == "" {
		return nil, ErrAppNameRequired
	}
	if modelName == "" {
		return nil, ErrModelNameRequired
	}
	if req == nil {
		return nil, ErrSuggestRequestNeeded
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, ErrQueryRequired
	}

	result, err := c.Request(ctx, "GET", fmt.Sprintf("/v3/openapi/apps/%s/suggest/%s/search", appName, modelName), req.ToQuery(), nil, nil)
	if err != nil {
		return nil, err
	}

	var response SuggestResponse
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("opensearchx: parse suggest response failed: %w", err)
	}
	return &response, nil
}

func (c *client) Hint(ctx context.Context, appName string, req *HintRequest) (*HintResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, ErrAppNameRequired
	}
	if req == nil {
		return nil, ErrHintRequestRequired
	}

	result, err := c.Request(ctx, "GET", fmt.Sprintf("/v3/openapi/apps/%s/actions/hint", appName), req.ToQuery(), nil, nil)
	if err != nil {
		return nil, err
	}

	var response HintResponse
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("opensearchx: parse hint response failed: %w", err)
	}
	return &response, nil
}

func (c *client) HotSearch(ctx context.Context, appName string, req *HotSearchRequest) (*HotSearchResponse, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, ErrAppNameRequired
	}
	if req == nil {
		return nil, ErrHotRequestRequired
	}

	result, err := c.Request(ctx, "GET", fmt.Sprintf("/v3/openapi/apps/%s/actions/hot", appName), req.ToQuery(), nil, nil)
	if err != nil {
		return nil, err
	}

	var response HotSearchResponse
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("opensearchx: parse hot search response failed: %w", err)
	}
	return &response, nil
}

func (c *client) Request(ctx context.Context, method, pathname string, query map[string]string, headers map[string]string, body any) (map[string]any, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	method = strings.TrimSpace(strings.ToUpper(method))
	pathname = strings.TrimSpace(pathname)
	if method == "" {
		return nil, ErrRequestMethodRequired
	}
	if pathname == "" {
		return nil, ErrRequestPathRequired
	}
	return c.requester.Do(ctx, requestSpec{
		Method:  method,
		Path:    pathname,
		Query:   cloneMap(query),
		Headers: cloneMap(headers),
		Body:    body,
	})
}

func parseResponse(result map[string]any, response any) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, response)
}

func prepareConfig(conf *Config) (*Config, error) {
	if conf == nil {
		return nil, ErrNilConfig
	}

	cloned := *conf
	cloned.Endpoint = strings.TrimSpace(cloned.Endpoint)
	cloned.Protocol = strings.TrimSpace(strings.ToLower(cloned.Protocol))
	cloned.AccessKeyID = strings.TrimSpace(cloned.AccessKeyID)
	cloned.AccessKeySecret = strings.TrimSpace(cloned.AccessKeySecret)
	cloned.SecurityToken = strings.TrimSpace(cloned.SecurityToken)
	cloned.UserAgent = strings.TrimSpace(cloned.UserAgent)

	switch {
	case cloned.Endpoint == "":
		return nil, ErrEndpointRequired
	case cloned.AccessKeyID == "":
		return nil, ErrAccessKeyIDRequired
	case cloned.AccessKeySecret == "":
		return nil, ErrAccessKeySecretNeeded
	}

	if cloned.Protocol == "" {
		cloned.Protocol = defaultProtocol
	}
	if cloned.Protocol != "http" && cloned.Protocol != "https" {
		return nil, ErrInvalidProtocol
	}
	if cloned.ConnectTimeout <= 0 {
		cloned.ConnectTimeout = defaultConnectTimeout
	}
	if cloned.ReadTimeout <= 0 {
		cloned.ReadTimeout = defaultReadTimeout
	}
	if cloned.MaxIdleConns <= 0 {
		cloned.MaxIdleConns = defaultMaxIdleConns
	}
	if cloned.newRequester == nil {
		cloned.newRequester = func(cfg *Config) requester {
			return &teaRequester{config: cfg}
		}
	}

	return &cloned, nil
}

type teaRequester struct {
	config *Config
}

func (r *teaRequester) Do(ctx context.Context, req requestSpec) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runtime := &teaUtil.RuntimeOptions{
		ConnectTimeout: tea.Int(int(r.config.ConnectTimeout / time.Millisecond)),
		ReadTimeout:    tea.Int(int(r.config.ReadTimeout / time.Millisecond)),
		MaxIdleConns:   tea.Int(r.config.MaxIdleConns),
		Autoretry:      tea.Bool(false),
		IgnoreSSL:      tea.Bool(strings.EqualFold(r.config.Protocol, "http")),
	}

	request := tea.NewRequest()
	request.Protocol = tea.String(strings.ToUpper(r.config.Protocol))
	request.Method = tea.String(req.Method)
	request.Pathname = tea.String(req.Path)
	request.Headers = map[string]*string{
		"user-agent":         teaUtil.GetUserAgent(tea.String(r.config.UserAgent)),
		"Date":               opensearchutil.GetDate(),
		"host":               tea.String(r.config.Endpoint),
		"X-Opensearch-Nonce": teaUtil.GetNonce(),
	}
	for key, value := range req.Headers {
		if value = strings.TrimSpace(value); value != "" {
			request.Headers[key] = tea.String(value)
		}
	}
	if r.config.SecurityToken != "" {
		request.Headers["X-Opensearch-Security-Token"] = tea.String(r.config.SecurityToken)
	}
	if len(req.Query) > 0 {
		query := make(map[string]interface{}, len(req.Query))
		for key, value := range req.Query {
			query[key] = value
		}
		request.Query = teaUtil.StringifyMapValue(query)
	}
	if req.Body != nil {
		body := teaUtil.ToJSONString(req.Body)
		request.Headers["Content-MD5"] = opensearchutil.GetContentMD5(body)
		request.Headers["Content-Type"] = tea.String("application/json")
		request.Body = tea.ToReader(body)
	}

	request.Headers["Authorization"] = opensearchutil.GetSignature(request, tea.String(r.config.AccessKeyID), tea.String(r.config.AccessKeySecret))

	response, err := tea.DoRequest(request, map[string]interface{}{
		"timeouted":      "retry",
		"readTimeout":    tea.IntValue(runtime.ReadTimeout),
		"connectTimeout": tea.IntValue(runtime.ConnectTimeout),
		"maxIdleConns":   tea.IntValue(runtime.MaxIdleConns),
		"retry": map[string]interface{}{
			"retryable":   false,
			"maxAttempts": 1,
		},
		"backoff": map[string]interface{}{
			"policy": "no",
			"period": 1,
		},
		"ignoreSSL": tea.BoolValue(runtime.IgnoreSSL),
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bodyString, err := teaUtil.ReadAsString(response.Body)
	if err != nil {
		return nil, err
	}
	if tea.BoolValue(teaUtil.Is4xx(response.StatusCode)) || tea.BoolValue(teaUtil.Is5xx(response.StatusCode)) {
		return nil, tea.NewSDKError(map[string]interface{}{
			"message": tea.StringValue(response.StatusMessage),
			"data":    tea.StringValue(bodyString),
			"code":    tea.IntValue(response.StatusCode),
		})
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(tea.StringValue(bodyString)), &parsed); err != nil {
		return nil, err
	}
	return map[string]any{
		"body":    parsed,
		"headers": response.Headers,
	}, nil
}

func appendQueryPart(parts *[]string, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		*parts = append(*parts, key+":"+value)
	}
}

func appendClause(parts *[]string, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		*parts = append(*parts, key+"="+value)
	}
}

func appendMap(values map[string]string, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		values[key] = value
	}
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func formatFilterValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		return strings.Join(typed, ",")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(value)
	}
}

func positiveIntString(value int) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", value)
}

func isQuoted(value string) bool {
	return strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")
}
