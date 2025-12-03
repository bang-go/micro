package httpx

import "github.com/bang-go/opt"

type RequestBasicAuth struct {
	Username string `json:"username"` //base认证username
	Password string `json:"password"` //base认证password
}
type requestOptions struct {
	baseAuth *RequestBasicAuth //base认证
	//todo: 代理
}

func WithBasicAuth(auth *RequestBasicAuth) opt.Option[requestOptions] {
	return opt.OptionFunc[requestOptions](func(o *requestOptions) {
		o.baseAuth = auth
	})
}
