package gateway

import (
	"context"
	"net/http"

	"google.golang.org/grpc/metadata"
)

// Paralus Gateway annotations | Paralus 网关注解.
const (
	// GatewayRequest marks a request that passed the gateway | 标记经过网关的请求.
	GatewayRequest = "x-gateway-request"
	// GatewayURL is the escaped request path | 请求的转义路径.
	GatewayURL = "x-gateway-url"
	// GatewaySessionCookie is the session cookie name | 会话 cookie 名.
	GatewaySessionCookie = "ory_kratos_session"
	// GatewayAPIKey carries the session token | 会话令牌 header.
	GatewayAPIKey = "X-Session-Token"
	// APIKey carries the key id | 携带 key id 的请求头.
	APIKey = "X-API-KEYID"
	// APIKeyToken carries the api token | api 令牌 header.
	APIKeyToken = "X-API-TOKEN"
	// GatewayMethod is the HTTP method | HTTP 方法.
	GatewayMethod = "x-gateway-method"
	// UserAgent is the client user agent | 客户端 User-Agent.
	UserAgent = "x-gateway-user-agent"
	// Host is the request host | 请求 Host.
	Host = "x-gateway-host"
	// RemoteAddr is the client address | 客户端地址.
	RemoteAddr = "x-gateway-remote-addr"
)

// ParalusGatewayAnnotator adds paralus gateway specific annotations | ParalusGatewayAnnotator 注入 paralus 网关特有的元数据.
var ParalusGatewayAnnotator = func(ctx context.Context, r *http.Request) metadata.MD {
	return metadata.New(map[string]string{
		GatewayRequest: "true",
		GatewayURL:     r.URL.EscapedPath(),
		GatewayAPIKey:  r.Header.Get(GatewayAPIKey),
		APIKey:         r.Header.Get(APIKey),
		APIKeyToken:    r.Header.Get(APIKeyToken),
		GatewayMethod:  r.Method,
		UserAgent:      r.UserAgent(),
		Host:           r.Host,
		RemoteAddr:     r.RemoteAddr,
	})
}
