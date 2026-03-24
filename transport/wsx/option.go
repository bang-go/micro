package wsx

import (
	"context"
	"net/http"
	"time"

	"github.com/bang-go/opt"
)

// ------------------- Connect Options -------------------

type connectOptions struct {
	heartbeatInterval time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	sendBufferSize    int
	userID            string
	skipObservability bool
}

func WithHeartbeatInterval(d time.Duration) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.heartbeatInterval = d
	})
}

func WithReadTimeout(d time.Duration) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.readTimeout = d
	})
}

func WithWriteTimeout(d time.Duration) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.writeTimeout = d
	})
}

func WithSendBufferSize(size int) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.sendBufferSize = size
	})
}

func WithConnectUserID(userID string) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.userID = userID
	})
}

func withSkipObservability(skip bool) opt.Option[connectOptions] {
	return opt.OptionFunc[connectOptions](func(o *connectOptions) {
		o.skipObservability = skip
	})
}

// ------------------- Server Options -------------------

type serverOptions struct {
	checkOrigin func(r *http.Request) bool
	// beforeUpgrade 允许在升级前进行鉴权。如果返回 error，升级将被拒绝。
	beforeUpgrade func(r *http.Request) error
	// identify 在升级前提取业务身份。返回错误会拒绝升级。
	identify func(ctx context.Context, r *http.Request) (string, error)
	// onConnect 允许在连接建立后立即执行副作用逻辑。如果返回 error，连接将关闭。
	onConnect   func(ctx context.Context, c Connect, r *http.Request) error
	path        string
	connectOpts []opt.Option[connectOptions]
	hub         Hub
}

func WithServerCheckOrigin(f func(r *http.Request) bool) opt.Option[serverOptions] {
	return opt.OptionFunc[serverOptions](func(o *serverOptions) {
		o.checkOrigin = f
	})
}

func WithServerBeforeUpgrade(f func(r *http.Request) error) opt.Option[serverOptions] {
	return opt.OptionFunc[serverOptions](func(o *serverOptions) {
		o.beforeUpgrade = f
	})
}

func WithServerIdentify(f func(ctx context.Context, r *http.Request) (string, error)) opt.Option[serverOptions] {
	return opt.OptionFunc[serverOptions](func(o *serverOptions) {
		o.identify = f
	})
}

func WithServerOnConnect(f func(ctx context.Context, c Connect, r *http.Request) error) opt.Option[serverOptions] {
	return opt.OptionFunc[serverOptions](func(o *serverOptions) {
		o.onConnect = f
	})
}

func WithServerPath(path string) opt.Option[serverOptions] {
	return opt.OptionFunc[serverOptions](func(o *serverOptions) {
		o.path = path
	})
}

func WithServerConnectOption(opts ...opt.Option[connectOptions]) opt.Option[serverOptions] {
	return opt.OptionFunc[serverOptions](func(o *serverOptions) {
		o.connectOpts = append(o.connectOpts, opts...)
	})
}

func WithServerHub(h Hub) opt.Option[serverOptions] {
	return opt.OptionFunc[serverOptions](func(o *serverOptions) {
		o.hub = h
	})
}

// ------------------- Client Options -------------------

type clientOptions struct {
	dialTimeout          time.Duration
	reconnectInterval    time.Duration
	maxReconnectAttempts int
	httpHeader           http.Header
	connectOpts          []opt.Option[connectOptions]
}

func WithClientDialTimeout(d time.Duration) opt.Option[clientOptions] {
	return opt.OptionFunc[clientOptions](func(o *clientOptions) {
		o.dialTimeout = d
	})
}

func WithClientReconnectInterval(d time.Duration) opt.Option[clientOptions] {
	return opt.OptionFunc[clientOptions](func(o *clientOptions) {
		o.reconnectInterval = d
	})
}

func WithClientMaxReconnectAttempts(n int) opt.Option[clientOptions] {
	return opt.OptionFunc[clientOptions](func(o *clientOptions) {
		o.maxReconnectAttempts = n
	})
}

func WithClientHTTPHeader(header http.Header) opt.Option[clientOptions] {
	return opt.OptionFunc[clientOptions](func(o *clientOptions) {
		o.httpHeader = header
	})
}

func WithClientConnectOption(opts ...opt.Option[connectOptions]) opt.Option[clientOptions] {
	return opt.OptionFunc[clientOptions](func(o *clientOptions) {
		o.connectOpts = append(o.connectOpts, opts...)
	})
}
