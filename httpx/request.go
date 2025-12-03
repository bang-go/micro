package httpx

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Request 请求结构体
type Request struct {
	Url         string            `json:"url"`          // 请求url
	Method      string            `json:"method"`       //请求方法，GET/POST/PUT/DELETE/PATCH...
	Params      map[string]string `json:"params"`       //Query参数
	Body        string            `json:"body"`         //请求体
	Headers     map[string]string `json:"headers"`      // 请求头
	ContentType string            `json:"content_type"` //数据编码格式 //TODO:更多
	Files       map[string]string `json:"files"`        //TODO:文件
	Cookies     map[string]string `json:"cookies"`      //Cookies
	//Timeout     time.Duration     `json:"timeout"`      //超时时间
}

// 设置请求方式
func (r *Request) getMethod() (method string, err error) {
	if r.Method == "" {
		err = errors.New("method is empty")
		return
	}
	method = strings.ToUpper(r.Method)
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
	default:
		err = errors.New("unknown method")
		return
	}
	return
}

// 设置请求地址(拼接请求参数)
func (r *Request) getUrl() (us string, err error) {
	us = r.Url
	if r.Url == "" {
		err = errors.New("url is empty")
		return
	}
	if r.Params == nil {
		return
	}
	urlValues := url.Values{}
	httpUrl, err := url.Parse(r.Url)
	if err != nil {
		return
	}
	for key, value := range r.Params {
		urlValues.Set(key, value)
	}
	httpUrl.RawQuery = urlValues.Encode()
	us = httpUrl.String()
	return
}

// 设置请求头
func (r *Request) setHeaders(req *http.Request) {
	if r.Headers == nil {
		return
	}
	for key, value := range r.Headers {
		req.Header.Add(key, value)
	}
}

func (r *Request) setCookie(req *http.Request) {
	if r.Cookies == nil {
		return
	}
	for key, value := range r.Cookies {
		req.AddCookie(&http.Cookie{Name: key, Value: value})
	}
}

// 设置请求体
func (r *Request) getBody() *strings.Reader {
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	// 只有在有请求体时才设置 Content-Type
	if r.Body != "" {
		switch r.ContentType {
		case ContentRaw:
			// Raw 类型不设置 Content-Type，由用户自行设置
		case ContentForm:
			r.Headers["Content-Type"] = "application/x-www-form-urlencoded"
		case ContentJson:
			r.Headers["Content-Type"] = "application/json"
		default: //默认json
			r.Headers["Content-Type"] = "application/json"
		}
	}
	return strings.NewReader(r.Body)
}
