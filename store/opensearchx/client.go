package opensearchx

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	opensearchutil "github.com/alibabacloud-go/opensearch-util/service"
	teaUtil "github.com/alibabacloud-go/tea-utils/service"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
)

// Config OpenSearch 客户端配置
type Config struct {
	// Endpoint OpenSearch 服务端点，例如: "opensearch-cn-hangzhou.aliyuncs.com"
	Endpoint string
	// Protocol 协议，默认为 "HTTP"
	Protocol string
	// AccessKeyId 访问密钥 ID
	AccessKeyId string
	// AccessKeySecret 访问密钥 Secret
	AccessKeySecret string
	// SecurityToken 安全令牌（可选，用于 STS）
	SecurityToken string
	// UserAgent 用户代理（可选）
	UserAgent string
	// ConnectTimeout 连接超时时间（毫秒），默认 5000
	ConnectTimeout int
	// ReadTimeout 读取超时时间（毫秒），默认 10000
	ReadTimeout int
	// MaxIdleConns 最大空闲连接数，默认 50
	MaxIdleConns int
}

const (
	// DefaultConnectTimeout 默认连接超时时间（毫秒）
	DefaultConnectTimeout = 5000
	// DefaultReadTimeout 默认读取超时时间（毫秒）
	DefaultReadTimeout = 10000
	// DefaultMaxIdleConns 默认最大空闲连接数
	DefaultMaxIdleConns = 50
)

// ResponseError 响应错误
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ResponseHeader 响应头
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

// SuggestResponse 下拉提示（搜索建议）响应
type SuggestResponse struct {
	Headers ResponseHeader `json:"headers"`
	Body    SuggestBody    `json:"body"`
}

// SuggestBody 下拉提示响应体
type SuggestBody struct {
	RequestId   string        `json:"request_id"`
	SearchTime  float64       `json:"searchtime"`
	Suggestions []SuggestItem `json:"suggestions"`
}

// SuggestItem 下拉提示项
type SuggestItem struct {
	Suggestion string `json:"suggestion"`
}

// HintResponse 底纹响应
type HintResponse struct {
	Headers ResponseHeader `json:"headers"`
	Body    HintBody       `json:"body"`
}

// HintBody 底纹响应体
type HintBody struct {
	RequestId  string     `json:"request_id"`
	SearchTime float64    `json:"searchtime"`
	Result     []HintItem `json:"result"` // 注意：官方返回的是 result 字段，不是 items
}

// HintItem 底纹项
type HintItem struct {
	Hint string `json:"hint"`         // 底纹关键词
	Pv   *int64 `json:"pv,omitempty"` // 浏览量（可选）
}

// HotSearchResponse 热搜响应
type HotSearchResponse struct {
	Headers ResponseHeader `json:"headers"`
	Body    HotSearchBody  `json:"body"`
}

// HotSearchBody 热搜响应体
type HotSearchBody struct {
	RequestId  string          `json:"request_id"`
	SearchTime float64         `json:"searchtime"`
	Result     []HotSearchItem `json:"result"` // 注意：官方返回的是 result 字段，不是 items
}

// HotSearchItem 热搜项
type HotSearchItem struct {
	Hot  string `json:"hot"`            // 热搜关键词
	Pv   *int64 `json:"pv,omitempty"`   // 浏览量（可选）
	Tags *int   `json:"tags,omitempty"` // 标签：新-0，热-1，爆-2（实时热搜模型返回）
}

// SearchResponse 搜索响应
type SearchResponse[T any] struct {
	Headers ResponseHeader `json:"headers"`
	Body    SearchBody[T]  `json:"body"`
}

// SearchBody 搜索响应体
type SearchBody[T any] struct {
	Error          []ResponseError `json:"error"`
	OpsRequestMisc string          `json:"ops_request_misc"`
	RequestId      string          `json:"request_id"`
	Status         string          `json:"status"`
	Trace          string          `json:"trace"`
	Result         SearchResult[T] `json:"result"`
}

// SearchResult 搜索结果
type SearchResult[T any] struct {
	ComputeCost []any   `json:"compute_cost"`
	Facet       []any   `json:"facet"`
	Num         uint32  `json:"num"`
	SearchTime  float64 `json:"searchtime"`
	Total       uint32  `json:"total"`
	ViewTotal   uint32  `json:"viewtotal"`
	Items       []T     `json:"items"`
}

// QueryClause 查询子句（结构化）
type QueryClause struct {
	// Index 索引名称，例如: "default"、"title"、"content" 等
	// 支持的查询类型通过索引名体现：
	//   - "default": 默认查询，支持分词
	//   - "phrase": 短语查询，精确匹配
	//   - "and": 与查询
	//   - "or": 或查询
	//   - "not": 非查询
	//   - "rank": 排序查询
	//   - "phrase_rank": 短语排序查询
	Index string
	// Value 查询值
	Value string
	// ExactMatch 是否精确匹配（添加引号），默认 true
	// 如果为 false，则不会在值周围添加引号
	ExactMatch bool
}

// String 将 QueryClause 转换为查询字符串
func (q *QueryClause) String() string {
	if q == nil || q.Index == "" || q.Value == "" {
		return ""
	}
	value := q.Value
	// 如果 ExactMatch 为 true 且值不包含空格，添加引号
	if q.ExactMatch && !strings.Contains(value, " ") && !strings.HasPrefix(value, "'") && !strings.HasSuffix(value, "'") {
		value = fmt.Sprintf("'%s'", value)
	}
	return fmt.Sprintf("%s:%s", q.Index, value)
}

// FilterClause 过滤子句（结构化）
type FilterClause struct {
	// Field 字段名
	Field string
	// Operator 运算符，支持: =, !=, >, >=, <, <=, IN, NOT IN
	Operator string
	// Value 值（可以是单个值或多个值，多个值用逗号分隔，用于 IN 操作）
	Value interface{}
	// Logic 逻辑运算符，用于连接多个条件，支持: AND, OR, NOT
	// 如果为空，则使用 AND
	Logic string
}

// String 将 FilterClause 转换为过滤字符串
func (f *FilterClause) String() string {
	if f == nil || f.Field == "" || f.Operator == "" {
		return ""
	}
	var valueStr string
	switch v := f.Value.(type) {
	case string:
		valueStr = v
	case int, int32, int64:
		valueStr = fmt.Sprintf("%d", v)
	case float32, float64:
		valueStr = fmt.Sprintf("%.2f", v)
	case []string:
		valueStr = strings.Join(v, ",")
	case []interface{}:
		var parts []string
		for _, item := range v {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		valueStr = strings.Join(parts, ",")
	default:
		valueStr = fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf("%s%s%s", f.Field, f.Operator, valueStr)
}

// SortClause 排序子句（结构化）
type SortClause struct {
	// Field 字段名
	Field string
	// Order 排序方式，支持: + (升序), - (降序), -RANK (按相关性降序)
	Order string
}

// String 将 SortClause 转换为排序字符串
func (s *SortClause) String() string {
	if s.Field == "" {
		return ""
	}
	order := s.Order
	if order == "" {
		order = "-"
	}
	return fmt.Sprintf("%s:%s", s.Field, order)
}

// DistinctClause 打散子句（结构化）
type DistinctClause struct {
	// Key 打散字段名
	Key string
	// Count 每个 key 最多返回的结果数
	Count int
	// Times 打散次数
	Times int
}

// String 将 DistinctClause 转换为打散字符串
func (d *DistinctClause) String() string {
	if d == nil || d.Key == "" {
		return ""
	}
	parts := []string{fmt.Sprintf("dist_key:%s", d.Key)}
	if d.Count > 0 {
		parts = append(parts, fmt.Sprintf("dist_count:%d", d.Count))
	}
	if d.Times > 0 {
		parts = append(parts, fmt.Sprintf("dist_times:%d", d.Times))
	}
	return strings.Join(parts, ",")
}

// AggregateClause 统计子句（结构化）
type AggregateClause struct {
	// GroupKey 分组字段名
	GroupKey string
	// AggFun 聚合函数，例如: count(), sum(price), max(price), min(price), avg(price)
	AggFun string
}

// String 将 AggregateClause 转换为统计字符串
func (a *AggregateClause) String() string {
	if a.GroupKey == "" || a.AggFun == "" {
		return ""
	}
	return fmt.Sprintf("group_key:%s,agg_fun:%s", a.GroupKey, a.AggFun)
}

// SearchRequest 搜索请求参数（结构化）
type SearchRequest struct {
	// Query 查询子句（必需），使用结构化类型
	Query *QueryClause
	// Filter 过滤子句（可选），使用结构化类型
	Filter *FilterClause
	// Sort 排序子句（可选），使用结构化类型
	Sort *SortClause
	// Distinct 打散子句（可选），使用结构化类型
	Distinct *DistinctClause
	// Aggregate 统计子句（可选），使用结构化类型
	Aggregate *AggregateClause
	// KVPairs 自定义子句（可选），例如: "key1:value1,key2:value2"
	// 用于传递自定义参数
	KVPairs string
	// Start 起始位置，默认 0
	Start int
	// Hit 返回数量，默认 10
	Hit int
	// Format 返回格式，默认 "fulljson"
	// 可选值: fulljson, json, xml
	Format string
	// Config 其他配置参数（可选），格式: "key1:value1,key2:value2"
	// 例如: "rerank_size:200"
	Config string
	// FetchFields 返回字段列表，多个字段用分号;分隔
	FetchFields string
	// QP 查询分析规则，多个规则用逗号,分隔
	QP string
	// Disable 关闭指定功能，格式: "function1;function2"
	// 支持: qp, summary, first_rank, second_rank, re_search
	Disable string
	// FirstRankName 基础排序表达式名称
	FirstRankName string
	// SecondRankName 业务排序表达式名称
	SecondRankName string
	// UserID 用户ID（需要URL编码）
	UserID string
	// ABTest A/B测试参数
	ABTest string
	// RawQuery 原始查询词（用于算法训练）
	RawQuery string
	// SearchStrategy 查询策略名称（多路搜索）
	SearchStrategy string
	// ReSearch 重查策略，格式: "strategy:threshold,params:total_hits#${COUNT}"
	ReSearch string
	// Biz 业务信息，格式: "type:$TYPE"
	Biz string
	// Summary 摘要配置
	Summary *SummaryConfig
	// FromRequestID 引导来源的request_id
	FromRequestID string
	// VectorThreshold 向量分数阈值（浮点型）
	VectorThreshold *float64
}

// SummaryConfig 摘要配置
type SummaryConfig struct {
	// SummaryField 要做摘要的字段（必需）
	SummaryField string
	// SummaryElement 飘红标签，html标签去掉左右尖括号，例如: "em"
	SummaryElement string
	// SummaryEllipsis 摘要的结尾省略符，默认 "…"
	SummaryEllipsis string
	// SummarySnipped 选取的摘要片段个数，默认 1
	SummarySnipped int
	// SummaryLen 摘要要展示的片段长度
	SummaryLen string
	// SummaryElementPrefix 飘红的前缀，必须是完整的html标签，如 "<em>"
	SummaryElementPrefix string
	// SummaryElementPostfix 飘红的后缀，必须是完整的html标签，如 "</em>"
	SummaryElementPostfix string
}

// String 将 SummaryConfig 转换为字符串格式
func (s *SummaryConfig) String() string {
	if s.SummaryField == "" {
		return ""
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("summary_field:%s", s.SummaryField))
	if s.SummaryElement != "" {
		parts = append(parts, fmt.Sprintf("summary_element:%s", s.SummaryElement))
	}
	if s.SummaryEllipsis != "" {
		parts = append(parts, fmt.Sprintf("summary_ellipsis:%s", s.SummaryEllipsis))
	}
	if s.SummarySnipped > 0 {
		parts = append(parts, fmt.Sprintf("summary_snipped:%d", s.SummarySnipped))
	}
	if s.SummaryLen != "" {
		parts = append(parts, fmt.Sprintf("summary_len:%s", s.SummaryLen))
	}
	if s.SummaryElementPrefix != "" {
		parts = append(parts, fmt.Sprintf("summary_element_prefix:%s", s.SummaryElementPrefix))
	}
	if s.SummaryElementPostfix != "" {
		parts = append(parts, fmt.Sprintf("summary_element_postfix:%s", s.SummaryElementPostfix))
	}
	return strings.Join(parts, ",")
}

// String 将 SearchRequest 转换为查询字符串
// 格式: "query=default:'关键词'&&config=start:0,hit:10,format:fulljson"
// 注意：query 子句应该在前面，config 子句在后面
// 返回的字符串是未编码的，将作为 URL 的 query 参数值使用，由 SDK 负责编码
func (r *SearchRequest) String() string {
	if r.Query == nil {
		return ""
	}

	var clauses []string

	// Config 子句（第一个子句，不包含 query= 前缀）
	var configParts []string
	start := r.Start
	if start < 0 {
		start = 0
	}
	hit := r.Hit
	if hit <= 0 {
		hit = 10
	}
	format := r.Format
	if format == "" {
		format = "fulljson"
	}
	configParts = append(configParts,
		fmt.Sprintf("start:%d", start),
		fmt.Sprintf("hit:%d", hit),
		fmt.Sprintf("format:%s", format),
	)
	// 添加其他配置参数
	if r.Config != "" {
		configParts = append(configParts, r.Config)
	}
	// Query 子句（必需，作为第一个子句，带 query= 前缀）
	queryStr := r.Query.String()
	if queryStr != "" {
		clauses = append(clauses, fmt.Sprintf("query=%s", queryStr))
	}

	// Config 子句（第二个子句，不包含 query= 前缀）
	clauses = append(clauses, fmt.Sprintf("config=%s", strings.Join(configParts, ",")))

	// Filter 子句（可选）
	if r.Filter != nil {
		filterStr := r.Filter.String()
		if filterStr != "" {
			clauses = append(clauses, fmt.Sprintf("filter=%s", filterStr))
		}
	}

	// Sort 子句（可选）
	if r.Sort != nil {
		sortStr := r.Sort.String()
		if sortStr != "" {
			clauses = append(clauses, fmt.Sprintf("sort=%s", sortStr))
		}
	}

	// Distinct 子句（可选）
	if r.Distinct != nil {
		distinctStr := r.Distinct.String()
		if distinctStr != "" {
			clauses = append(clauses, fmt.Sprintf("distinct=%s", distinctStr))
		}
	}

	// Aggregate 子句（可选）
	if r.Aggregate != nil {
		aggregateStr := r.Aggregate.String()
		if aggregateStr != "" {
			clauses = append(clauses, fmt.Sprintf("aggregate=%s", aggregateStr))
		}
	}

	// KVPairs 子句（可选）
	if r.KVPairs != "" {
		clauses = append(clauses, fmt.Sprintf("kvpairs=%s", r.KVPairs))
	}

	// 使用 && 连接所有子句
	return strings.Join(clauses, "&&")
}

// HintRequest 底纹请求参数（结构化）
type HintRequest struct {
	// Hit 返回数量，默认 10，范围为 1 到 30
	Hit int
	// SortType 排序类型，可选值为 "default"（默认）、"pv"（按浏览量排序）等
	SortType string
	// UserID 用户 ID，用于个性化推荐（可选）
	UserID string
	// ModelName 模型名称，若有多个模型可指定（可选）
	ModelName string
}

// ToQuery 将 HintRequest 转换为查询参数 map
func (r *HintRequest) ToQuery() map[string]interface{} {
	query := make(map[string]interface{})
	if r.Hit > 0 {
		query["hit"] = r.Hit
	}
	if r.SortType != "" {
		query["sort_type"] = r.SortType
	}
	if r.UserID != "" {
		query["user_id"] = r.UserID
	}
	if r.ModelName != "" {
		query["model_name"] = r.ModelName
	}
	return query
}

// HotSearchRequest 热搜请求参数（结构化）
type HotSearchRequest struct {
	// Hit 返回数量，默认 10，范围为 1 到 30
	Hit int
	// SortType 排序类型，可选值为 "default"（默认）、"pv"（按浏览量排序）等
	SortType string
	// UserID 用户 ID，用于个性化推荐（可选）
	UserID string
	// ModelName 模型名称，若有多个模型可指定（可选）
	ModelName string
}

// ToQuery 将 HotSearchRequest 转换为查询参数 map
func (r *HotSearchRequest) ToQuery() map[string]interface{} {
	query := make(map[string]interface{})
	if r.Hit > 0 {
		query["hit"] = r.Hit
	}
	if r.SortType != "" {
		query["sort_type"] = r.SortType
	}
	if r.UserID != "" {
		query["user_id"] = r.UserID
	}
	if r.ModelName != "" {
		query["model_name"] = r.ModelName
	}
	return query
}

// SuggestRequest 下拉提示请求参数（结构化）
type SuggestRequest struct {
	// Query 查询关键词
	Query string
	// Hit 返回数量，默认 10
	Hit int
	// 其他参数（可选）
	Extra map[string]interface{}
}

// ToQuery 将 SuggestRequest 转换为查询参数 map
func (r *SuggestRequest) ToQuery() map[string]interface{} {
	query := make(map[string]interface{})
	if r.Query != "" {
		query["query"] = r.Query
	}
	if r.Hit > 0 {
		query["hit"] = r.Hit
	}
	if r.Extra != nil {
		for k, v := range r.Extra {
			query[k] = v
		}
	}
	return query
}

// Client OpenSearch 客户端接口
type Client interface {
	// Search 搜索文档
	// appName: 应用名称
	// req: 搜索请求参数
	// 返回: 原始响应 map，可以使用 SearchTyped 辅助函数进行类型转换
	Search(appName string, req *SearchRequest) (map[string]interface{}, error)
	// Suggest 获取下拉提示（搜索建议）
	// appName: 应用名称
	// modelName: 模型名称
	// req: 下拉提示请求参数
	Suggest(appName, modelName string, req *SuggestRequest) (*SuggestResponse, error)
	// Hint 获取底纹
	// appName: 应用名称
	// req: 底纹请求参数
	Hint(appName string, req *HintRequest) (*HintResponse, error)
	// HotSearch 获取热搜
	// appName: 应用名称
	// req: 热搜请求参数
	HotSearch(appName string, req *HotSearchRequest) (*HotSearchResponse, error)
	// Request 发送原始请求（用于高级操作）
	Request(method, pathname string, query map[string]interface{}, headers map[string]string, body interface{}) (map[string]interface{}, error)
}

// ClientEntity 客户端实现
type ClientEntity struct {
	*Config
	client *internalClient
}

// internalClient 内部客户端（封装阿里云 SDK）
type internalClient struct {
	Endpoint   *string
	Protocol   *string
	UserAgent  *string
	Credential credential.Credential
}

// New creates a new OpenSearch client
func New(config *Config) (Client, error) {
	if config == nil {
		return nil, errors.New("osx: config is required")
	}
	if config.Endpoint == "" {
		return nil, errors.New("osx: endpoint is required")
	}
	if config.AccessKeyId == "" {
		return nil, errors.New("osx: access key id is required")
	}
	if config.AccessKeySecret == "" {
		return nil, errors.New("osx: access key secret is required")
	}

	// Set defaults
	if config.Protocol == "" {
		config.Protocol = "HTTP"
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = DefaultConnectTimeout
	}
	if config.ReadTimeout <= 0 {
		config.ReadTimeout = DefaultReadTimeout
	}
	if config.MaxIdleConns <= 0 {
		config.MaxIdleConns = DefaultMaxIdleConns
	}

	// Create credential config
	credentialType := "access_key"
	if config.SecurityToken != "" {
		credentialType = "sts"
	}

	credentialConfig := &credential.Config{
		AccessKeyId:     tea.String(config.AccessKeyId),
		Type:            tea.String(credentialType),
		AccessKeySecret: tea.String(config.AccessKeySecret),
	}
	if config.SecurityToken != "" {
		credentialConfig.SecurityToken = tea.String(config.SecurityToken)
	}

	cred, err := credential.NewCredential(credentialConfig)
	if err != nil {
		return nil, fmt.Errorf("osx: create credential failed: %w", err)
	}

	internalClient := &internalClient{
		Endpoint:   tea.String(config.Endpoint),
		Protocol:   tea.String(config.Protocol),
		Credential: cred,
	}
	if config.UserAgent != "" {
		internalClient.UserAgent = tea.String(config.UserAgent)
	}

	return &ClientEntity{
		Config: config,
		client: internalClient,
	}, nil
}

// Search 搜索文档（使用结构化请求）
func (c *ClientEntity) Search(appName string, req *SearchRequest) (map[string]interface{}, error) {
	if appName == "" {
		return nil, errors.New("appName 不能为空")
	}
	if req == nil {
		return nil, errors.New("SearchRequest 不能为 nil")
	}
	if req.Query == nil {
		return nil, errors.New("SearchRequest.Query 不能为 nil")
	}

	pathname := fmt.Sprintf("/v3/openapi/apps/%s/search", appName)
	// 将结构化请求转换为查询字符串
	queryStr := req.String()
	queryParams := map[string]interface{}{
		"query": queryStr,
	}
	result, err := c.Request("GET", pathname, queryParams, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	return result, nil
}

// SearchTyped 搜索文档（类型化辅助函数）
// 使用示例:
//
//	req := &opensearch.SearchRequest{
//	    Query: "default:'辣椒'",
//	    Start: 0,
//	    Hit:   10,
//	    Format: "fulljson",
//	}
//	response, err := opensearch.SearchTyped[YourType](client, "appName", req)
func SearchTyped[T any](client Client, appName string, req *SearchRequest) (*SearchResponse[T], error) {
	result, err := client.Search(appName, req)
	if err != nil {
		return nil, err
	}

	var response SearchResponse[T]
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &response, nil
}

// Suggest 获取下拉提示（搜索建议）
func (c *ClientEntity) Suggest(appName, modelName string, req *SuggestRequest) (*SuggestResponse, error) {
	if appName == "" {
		return nil, errors.New("appName 不能为空")
	}
	if modelName == "" {
		return nil, errors.New("modelName 不能为空")
	}
	if req == nil {
		return nil, errors.New("SuggestRequest 不能为 nil")
	}

	pathname := fmt.Sprintf("/v3/openapi/apps/%s/suggest/%s/search", appName, modelName)
	query := req.ToQuery()
	result, err := c.Request("GET", pathname, query, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("获取下拉提示失败: %w", err)
	}

	var response SuggestResponse
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &response, nil
}

// Hint 获取底纹
func (c *ClientEntity) Hint(appName string, req *HintRequest) (*HintResponse, error) {
	if appName == "" {
		return nil, errors.New("appName 不能为空")
	}
	if req == nil {
		return nil, errors.New("HintRequest 不能为 nil")
	}

	pathname := fmt.Sprintf("/v3/openapi/apps/%s/actions/hint", appName)
	query := req.ToQuery()
	result, err := c.Request("GET", pathname, query, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("获取底纹失败: %w", err)
	}

	var response HintResponse
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &response, nil
}

// HotSearch 获取热搜
func (c *ClientEntity) HotSearch(appName string, req *HotSearchRequest) (*HotSearchResponse, error) {
	if appName == "" {
		return nil, errors.New("appName 不能为空")
	}
	if req == nil {
		return nil, errors.New("HotSearchRequest 不能为 nil")
	}

	pathname := fmt.Sprintf("/v3/openapi/apps/%s/actions/hot", appName)
	query := req.ToQuery()
	result, err := c.Request("GET", pathname, query, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("获取热搜失败: %w", err)
	}

	var response HotSearchResponse
	if err := parseResponse(result, &response); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &response, nil
}

// Request 发送原始请求（用于高级操作）
func (c *ClientEntity) Request(method, pathname string, query map[string]interface{}, headers map[string]string, body interface{}) (map[string]interface{}, error) {
	// 创建运行时选项
	runtime := &teaUtil.RuntimeOptions{
		ConnectTimeout: tea.Int(c.ConnectTimeout),
		ReadTimeout:    tea.Int(c.ReadTimeout),
		MaxIdleConns:   tea.Int(c.MaxIdleConns),
		Autoretry:      tea.Bool(false),
		IgnoreSSL:      tea.Bool(false),
	}

	// 转换 headers
	var teaHeaders map[string]*string
	if len(headers) > 0 {
		teaHeaders = make(map[string]*string)
		for k, v := range headers {
			teaHeaders[k] = tea.String(v)
		}
	}

	return c.client.request(tea.String(method), tea.String(pathname), query, teaHeaders, body, runtime)
}

// parseResponse 解析响应（包级别函数，用于类型化搜索）
func parseResponse(result map[string]interface{}, response interface{}) error {
	// 将 map 转换为 JSON 再解析为结构体
	jsonData, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("序列化响应失败: %w", err)
	}

	if err := json.Unmarshal(jsonData, response); err != nil {
		return fmt.Errorf("反序列化响应失败: %w", err)
	}

	return nil
}

// ========== 内部客户端方法 ==========

// request 发送请求（内部方法）
func (c *internalClient) request(method *string, pathname *string, query map[string]interface{}, headers map[string]*string, body interface{}, runtime *teaUtil.RuntimeOptions) (_result map[string]interface{}, _err error) {
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
			"maxAttempts": tea.IntValue(teaUtil.DefaultNumber(runtime.MaxAttempts, tea.Int(3))),
		},
		"backoff": map[string]interface{}{
			"policy": tea.StringValue(teaUtil.DefaultString(runtime.BackoffPolicy, tea.String("no"))),
			"period": tea.IntValue(teaUtil.DefaultNumber(runtime.BackoffPeriod, tea.Int(1))),
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
			accesskeyId, _err := c.getAccessKeyId()
			if _err != nil {
				return _result, _err
			}

			accessKeySecret, _err := c.getAccessKeySecret()
			if _err != nil {
				return _result, _err
			}

			request_.Protocol = teaUtil.DefaultString(c.Protocol, tea.String("HTTP"))
			request_.Method = method
			request_.Pathname = pathname
			request_.Headers = tea.Merge(map[string]*string{
				"user-agent":         c.getUserAgent(),
				"Date":               opensearchutil.GetDate(),
				"host":               teaUtil.DefaultString(c.Endpoint, tea.String("opensearch-cn-hangzhou.aliyuncs.com")),
				"X-Opensearch-Nonce": teaUtil.GetNonce(),
			}, headers)
			if !tea.BoolValue(teaUtil.IsUnset(query)) {
				request_.Query = teaUtil.StringifyMapValue(query)
			}

			if !tea.BoolValue(teaUtil.IsUnset(body)) {
				reqBody := teaUtil.ToJSONString(body)
				request_.Headers["Content-MD5"] = opensearchutil.GetContentMD5(reqBody)
				request_.Headers["Content-Type"] = tea.String("application/json")
				request_.Body = tea.ToReader(reqBody)
			}

			request_.Headers["Authorization"] = opensearchutil.GetSignature(request_, accesskeyId, accessKeySecret)

			response_, _err := tea.DoRequest(request_, _runtime)
			if _err != nil {
				return _result, _err
			}
			objStr, _err := teaUtil.ReadAsString(response_.Body)
			if _err != nil {
				return _result, _err
			}

			objStrValue := tea.StringValue(objStr)
			if tea.BoolValue(teaUtil.Is4xx(response_.StatusCode)) || tea.BoolValue(teaUtil.Is5xx(response_.StatusCode)) {
				_err = tea.NewSDKError(map[string]interface{}{
					"message": tea.StringValue(response_.StatusMessage),
					"data":    objStrValue,
					"code":    tea.IntValue(response_.StatusCode),
				})
				return _result, _err
			}

			obj := teaUtil.ParseJSON(objStr)
			res := teaUtil.AssertAsMap(obj)
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

// getUserAgent 获取用户代理
func (c *internalClient) getUserAgent() (_result *string) {
	userAgent := teaUtil.GetUserAgent(c.UserAgent)
	_result = userAgent
	return _result
}

// getAccessKeyId 获取访问密钥 ID
func (c *internalClient) getAccessKeyId() (_result *string, _err error) {
	if tea.BoolValue(teaUtil.IsUnset(c.Credential)) {
		return _result, _err
	}

	accessKeyId, _err := c.Credential.GetAccessKeyId()
	if _err != nil {
		return _result, _err
	}

	_result = accessKeyId
	return _result, _err
}

// getAccessKeySecret 获取访问密钥 Secret
func (c *internalClient) getAccessKeySecret() (_result *string, _err error) {
	if tea.BoolValue(teaUtil.IsUnset(c.Credential)) {
		return _result, _err
	}

	secret, _err := c.Credential.GetAccessKeySecret()
	if _err != nil {
		return _result, _err
	}

	_result = secret
	return _result, _err
}
