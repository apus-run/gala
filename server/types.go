// Package server defines the transport-server contract shared by the http,
// grpc and gateway implementations | server 定义 http、grpc 与 gateway 等传输实现共用的传输服务契约.
//
// # Notes | 说明
//
// [Server] is the lifecycle contract (Start / Stop / Health); [Endpointer]
// exposes the address to register with a discovery system. Concrete servers
// live in the server/http, server/grpc and server/gateway subpackages.
// [Server] 是生命周期契约（Start / Stop / Health）；[Endpointer] 向注册中心
// 暴露服务地址。具体实现位于 server/http、server/grpc 与 server/gateway 子包。
package server

import (
	"context"
	"net/url"
)

// Server is a transport server with start/stop lifecycle and a health probe | 传输服务器：启动/停止生命周期与健康检查.
type Server interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health() bool
}

// Endpointer is a service with a registrable endpoint | 提供可注册端点的服务.
type Endpointer interface {
	Endpoint() (*url.URL, error)
}
