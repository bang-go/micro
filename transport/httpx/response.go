package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type RequestSnapshot struct {
	Method string
	URL    string
	Header http.Header
	Host   string
}

type Response struct {
	StatusCode int
	Status     string
	Header     http.Header
	Cookies    []*http.Cookie
	Body       []byte
	Duration   time.Duration
	Request    *RequestSnapshot
}

func (r *Response) Success() bool {
	return r != nil && r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices
}

func (r *Response) Text() string {
	if r == nil {
		return ""
	}
	return string(r.Body)
}

func (r *Response) DecodeJSON(dst any) error {
	if r == nil {
		return ErrNilResponse
	}
	if dst == nil {
		return errors.New("httpx: decode target is nil")
	}
	if err := json.Unmarshal(r.Body, dst); err != nil {
		return err
	}
	return nil
}

func newResponse(req *http.Request, resp *http.Response, body []byte, duration time.Duration) *Response {
	return &Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     cloneHeader(resp.Header),
		Cookies:    cloneCookies(resp.Cookies()),
		Body:       append([]byte(nil), body...),
		Duration:   duration,
		Request: &RequestSnapshot{
			Method: req.Method,
			URL:    redactedURLString(req.URL),
			Header: redactHeader(req.Header),
			Host:   req.Host,
		},
	}
}
