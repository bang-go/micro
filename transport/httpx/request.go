package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type BasicAuth struct {
	Username string
	Password string
}

type Request struct {
	Method string
	URL    string
	Query  url.Values
	Header http.Header
	Body   io.Reader

	Cookies   []*http.Cookie
	BasicAuth *BasicAuth
	Host      string
}

func (r *Request) Build(ctx context.Context) (*http.Request, error) {
	if r == nil {
		return nil, ErrNilRequest
	}
	if err := validateContext(ctx); err != nil {
		return nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		return nil, ErrRequestMethodRequired
	}
	rawURL := strings.TrimSpace(r.URL)
	if rawURL == "" {
		return nil, ErrRequestURLRequired
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("httpx: parse url: %w", err)
	}
	if !parsedURL.IsAbs() || parsedURL.Host == "" {
		return nil, ErrRequestAbsoluteURLRequired
	}

	query := parsedURL.Query()
	for key, values := range r.Query {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	parsedURL.RawQuery = query.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), r.Body)
	if err != nil {
		return nil, fmt.Errorf("httpx: build request: %w", err)
	}

	httpReq.Header = cloneHeader(r.Header)
	if r.Host != "" {
		httpReq.Host = r.Host
	}
	if r.BasicAuth != nil {
		httpReq.SetBasicAuth(r.BasicAuth.Username, r.BasicAuth.Password)
	}
	for _, cookie := range r.Cookies {
		if cookie == nil {
			continue
		}
		httpReq.AddCookie(cloneCookie(cookie))
	}
	return httpReq, nil
}

func (r *Request) SetHeader(key, value string) {
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Set(key, value)
}

func (r *Request) AddHeader(key, value string) {
	if r.Header == nil {
		r.Header = make(http.Header)
	}
	r.Header.Add(key, value)
}

func (r *Request) AddCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
	r.Cookies = append(r.Cookies, cloneCookie(cookie))
}

func (r *Request) SetBody(contentType string, body []byte) {
	body = append([]byte(nil), body...)
	r.Body = bytes.NewReader(body)
	if contentType != "" {
		r.SetHeader("Content-Type", contentType)
	}
}

func (r *Request) SetJSONBody(value any) error {
	if r == nil {
		return ErrNilRequest
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("httpx: marshal json body: %w", err)
	}
	r.SetBody(ContentTypeJSON, body)
	return nil
}

func (r *Request) SetFormBody(values url.Values) {
	if values == nil {
		values = url.Values{}
	}
	r.SetBody(ContentTypeFormURLEncoded, []byte(values.Encode()))
}
