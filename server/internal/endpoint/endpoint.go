// Package endpoint builds and parses registry endpoint URLs; internal to the
// server tree | endpoint 构建与解析注册端点 URL，为 server 体系内部包.
//
// # Notes | 说明
//
// [Scheme] appends "s" for secure transports ("http" + TLS gives "https");
// [ParseEndpoint] returns the host of the first URL whose scheme matches,
// or an empty string when none does. Internal package, not for external
// use.
// [Scheme] 为安全传输追加 "s"（"http"+TLS 得到 "https"）；[ParseEndpoint] 返回
// 第一个 scheme 匹配的 URL 的 host，都不匹配时返回空串。内部包，不对外使用。
package endpoint

import (
	"net/url"
)

// NewEndpoint new an Endpoint URL | NewEndpoint 新建一个 Endpoint URL.
func NewEndpoint(scheme, host string) *url.URL {
	return &url.URL{Scheme: scheme, Host: host}
}

// ParseEndpoint parses an Endpoint URL | ParseEndpoint 从端点列表解析指定 scheme 的 host.
func ParseEndpoint(endpoints []string, scheme string) (string, error) {
	for _, e := range endpoints {
		u, err := url.Parse(e)
		if err != nil {
			return "", err
		}

		if u.Scheme == scheme {
			return u.Host, nil
		}
	}
	return "", nil
}

// Scheme is the scheme of endpoint url.
// examples: scheme="http",isSecure=true get "https"
// Scheme 返回端点 URL 的 scheme。
// 示例：scheme="http"、isSecure=true 得到 "https".
func Scheme(scheme string, isSecure bool) string {
	if isSecure {
		return scheme + "s"
	}
	return scheme
}
