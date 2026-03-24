package httpx

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
)

var sensitiveHeaders = map[string]struct{}{
	"Authorization":       {},
	"Cookie":              {},
	"Proxy-Authorization": {},
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	return nil
}

func newSkipPathSet(defaults []string, extra []string) map[string]struct{} {
	skipPaths := make(map[string]struct{}, len(defaults)+len(extra))
	for _, path := range defaults {
		if path == "" {
			continue
		}
		skipPaths[path] = struct{}{}
	}
	for _, path := range extra {
		if path == "" {
			continue
		}
		skipPaths[path] = struct{}{}
	}
	return skipPaths
}

func matchesPath(skipPaths map[string]struct{}, path string) bool {
	_, ok := skipPaths[path]
	return ok
}

func defaultObservabilitySkipPaths(conf *ServerConfig) []string {
	paths := []string{"/metrics"}
	if !conf.DisableHealthEndpoint && conf.HealthPath != "" {
		paths = append(paths, conf.HealthPath)
	}
	return paths
}

func cloneHeader(header http.Header) http.Header {
	if header == nil {
		return nil
	}
	cloned := make(http.Header, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func redactHeader(header http.Header) http.Header {
	cloned := cloneHeader(header)
	for key := range cloned {
		if _, ok := sensitiveHeaders[http.CanonicalHeaderKey(key)]; ok {
			cloned[key] = []string{"REDACTED"}
		}
	}
	return cloned
}

func cloneCookie(cookie *http.Cookie) *http.Cookie {
	if cookie == nil {
		return nil
	}
	cloned := *cookie
	return &cloned
}

func cloneCookies(cookies []*http.Cookie) []*http.Cookie {
	if len(cookies) == 0 {
		return nil
	}
	cloned := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		cloned = append(cloned, cloneCookie(cookie))
	}
	return cloned
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	cloned := *u
	return &cloned
}

func redactedURLString(u *url.URL) string {
	if u == nil {
		return ""
	}
	cloned := cloneURL(u)
	cloned.User = nil
	return cloned.String()
}

func effectiveRequest(original *http.Request, resp *http.Response) *http.Request {
	if resp != nil && resp.Request != nil {
		return resp.Request
	}
	return original
}

func statusLabel(statusCode int) string {
	if statusCode <= 0 {
		return "error"
	}
	return strconv.Itoa(statusCode)
}

func remoteAddrHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
