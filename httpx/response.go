package httpx

import (
	"io"
	"net/http"
	"strings"
)

// Response 响应结构体
type Response struct {
	StatusCode int               `json:"status_code"` // 状态码
	Success    bool              `json:"success"`     // 响应状态
	Content    []byte            `json:"content"`     // 响应内容-字节
	Reason     string            `json:"reason"`      // 状态码说明
	Elapsed    float64           `json:"elapsed"`     // 请求耗时(秒)
	Headers    map[string]string `json:"headers"`     // 响应头
	Cookies    map[string]string `json:"cookies"`     // 响应Cookies
	Request    *Request          `json:"request"`     // 原始请求
}

// 组装响应体
func (r *Request) packResponse(res *http.Response, elapsed float64) (response *Response) {
	response = &Response{
		Request:    r,
		Elapsed:    elapsed,
		StatusCode: res.StatusCode,
	}
	response.Content, _ = io.ReadAll(res.Body)
	// 安全解析状态码说明
	statusParts := strings.SplitN(res.Status, " ", 2)
	if len(statusParts) > 1 {
		response.Reason = statusParts[1]
	} else {
		response.Reason = res.Status
	}
	response.Headers = map[string]string{}
	response.Cookies = map[string]string{}
	// 2xx 状态码都视为成功
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		response.Success = true
	}
	for key, value := range res.Header {
		response.Headers[key] = strings.Join(value, ";")
	}
	for _, v := range res.Cookies() {
		response.Cookies[v.Name] = v.Value
	}
	return
}
