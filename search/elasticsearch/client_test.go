package elasticsearch_test

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/bang-go/micro/search/elasticsearch"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

func TestClient(t *testing.T) {
	// 创建客户端
	client, err := elasticsearch.New(&elasticsearch.Config{
		Addresses: []string{""},
		Username:  "",
		Password:  "",
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	index := "test_index"

	// 清理：删除索引（如果存在）
	_, _ = client.DeleteIndex(index)

	// 1. 创建索引
	mapping := map[string]interface{}{
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type": "text",
				},
				"age": map[string]interface{}{
					"type": "integer",
				},
			},
		},
	}
	createResp, err := client.CreateIndex(index, mapping)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("索引创建成功: acknowledged=%v, index=%s", createResp.Acknowledged, createResp.Index)

	// 等待索引就绪
	time.Sleep(1 * time.Second)

	// 2. 索引文档
	document := map[string]interface{}{
		"name": "go-elasticsearch",
		"age":  1,
	}
	indexResp, err := client.Index(index, "1", document)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("文档索引成功: id=%s, result=%s", indexResp.Id_, indexResp.Result)

	// 等待索引刷新
	time.Sleep(1 * time.Second)

	// 3. 获取文档
	doc, err := client.Get(index, "1")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("获取文档成功: found=%v, id=%s, index=%s", doc.Found, doc.Id_, doc.Index_)
	if doc.Source_ != nil {
		var source map[string]interface{}
		if err := json.Unmarshal(doc.Source_, &source); err == nil {
			log.Printf("文档内容: %v", source)
		}
	}

	// 4. 更新文档
	updateDoc := map[string]interface{}{
		"age": 2,
	}
	updateResp, err := client.Update(index, "1", updateDoc)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("文档更新成功: id=%s, result=%s", updateResp.Id_, updateResp.Result)

	// 5. 搜索文档（类型化 API）
	typedRequest := &search.Request{
		Query: &types.Query{
			MatchAll: &types.MatchAllQuery{},
		},
	}
	typedResult, err := client.Search(ctx, index, typedRequest)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("搜索结果（类型化）: hits=%d", typedResult.Hits.Total.Value)

	// 7. 批量操作
	operations := []elasticsearch.BulkOperation{
		{
			Action:   "index",
			Index:    index,
			ID:       "2",
			Document: map[string]interface{}{"name": "bulk-test", "age": 10},
		},
		{
			Action:   "index",
			Index:    index,
			ID:       "3",
			Document: map[string]interface{}{"name": "bulk-test-2", "age": 20},
		},
	}
	bulkResp, err := client.Bulk(operations)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("批量操作成功: errors=%v, took=%dms", bulkResp.Errors, bulkResp.Took)

	// 等待索引刷新
	time.Sleep(1 * time.Second)

	// 8. 删除文档
	deleteResp, err := client.Delete(index, "1")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("文档删除成功: id=%s, result=%s", deleteResp.Id_, deleteResp.Result)

	// 9. 删除索引
	deleteIndexResp, err := client.DeleteIndex(index)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("索引删除成功: acknowledged=%v", deleteIndexResp.Acknowledged)
}
