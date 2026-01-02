package opensearchx_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/bang-go/micro/store/opensearchx"
)

func TestClient(t *testing.T) {
	// 从环境变量读取配置，如果没有设置则跳过测试
	endpoint := os.Getenv("OPENSEARCH_ENDPOINT")
	accessKeyId := os.Getenv("OPENSEARCH_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("OPENSEARCH_ACCESS_KEY_SECRET")
	appName := os.Getenv("OPENSEARCH_APP_NAME")

	if endpoint == "" || accessKeyId == "" || accessKeySecret == "" || appName == "" {
		t.Skip("跳过测试：需要设置环境变量 OPENSEARCH_ENDPOINT, OPENSEARCH_ACCESS_KEY_ID, OPENSEARCH_ACCESS_KEY_SECRET, OPENSEARCH_APP_NAME")
	}

	// 创建客户端
	client, err := opensearchx.New(&opensearchx.Config{
		Endpoint:        endpoint,
		AccessKeyId:     accessKeyId,
		AccessKeySecret: accessKeySecret,
	})
	if err != nil {
		t.Fatal(err)
	}

	modelName := ""

	// 测试 Search 方法（结构化请求，基本查询）
	searchReq := &opensearchx.SearchRequest{
		Query: &opensearchx.QueryClause{
			Index: "default",
			Value: "1",
		},
		Start:  0,
		Hit:    10,
		Format: "fulljson",
		// 以下子句为可选，根据实际需求添加
		// Filter: &opensearchx.FilterClause{
		// 	Field:    "status",
		// 	Operator: "=",
		// 	Value:    1,
		// 	Logic:    "AND",
		// },
		// Sort: &opensearchx.SortClause{
		// 	Field: "price",
		// 	Order: "-",
		// },
		// Distinct: &opensearchx.DistinctClause{
		// 	Key:   "user_id",
		// 	Count: 1,
		// 	Times: 1,
		// },
		// Aggregate: &opensearchx.AggregateClause{
		// 	GroupKey: "category",
		// 	AggFun:   "count()",
		// },
		// KVPairs:  "key1:value1,key2:value2",          // 自定义子句
		// Config:   "rerank_size:200",                  // 其他配置
	}
	searchResult, err := client.Search(appName, searchReq)
	if err != nil {
		fmt.Printf("Search 失败: %v\n", err)
	} else {
		fmt.Printf("Search 成功: %+v\n", searchResult)
	}

	// 测试 Suggest 方法（下拉提示，结构化请求）
	suggestReq := &opensearchx.SuggestRequest{
		Query: "1",
		Hit:   10,
	}
	suggestResponse, err := client.Suggest(appName, modelName, suggestReq)
	if err != nil {
		fmt.Printf("Suggest 失败: %v\n", err)
	} else {
		fmt.Printf("Suggest 成功: %+v\n", suggestResponse)
	}

	// 测试 Hint 方法（底纹，结构化请求）
	hintReq := &opensearchx.HintRequest{
		Hit:      10,
		SortType: "default",
		// UserID:    "user123",      // 可选：用户 ID
		// ModelName: "hint_model",   // 可选：模型名称
	}
	hintResponse, err := client.Hint(appName, hintReq)
	if err != nil {
		fmt.Printf("Hint 失败: %v\n", err)
	} else {
		fmt.Printf("Hint 成功: %+v\n", hintResponse)
	}

	// 测试 HotSearch 方法（热搜，结构化请求）
	hotSearchReq := &opensearchx.HotSearchRequest{
		Hit:      10,
		SortType: "default",
		// UserID:    "user123",      // 可选：用户 ID
		// ModelName: "hot_model",    // 可选：模型名称
	}
	hotSearchResponse, err := client.HotSearch(appName, hotSearchReq)
	if err != nil {
		fmt.Printf("HotSearch 失败: %v\n", err)
	} else {
		fmt.Printf("HotSearch 成功: %+v\n", hotSearchResponse)
	}
}
