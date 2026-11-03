package accesslog

import (
	"github.com/gin-gonic/gin"
)

// AccessLog carries the fields you may print; extend it as needed
// | AccessLog 携带可打印的请求信息，可根据需要自行增加字段。
type AccessLog struct {
	// PID is the server process id | 服务进程 id。
	PID string `json:"pid"`
	// Referer is the Referer request header | Referer 请求头。
	Referer string `json:"referer"`
	// Protocol is the request protocol (e.g. HTTP/1.1) | 请求协议（如 HTTP/1.1）。
	Protocol string `json:"protocol"`
	// Port is the server port | 服务端口。
	Port string `json:"port"`
	// IP is the server address | 服务端地址。
	IP string `json:"ip"`
	// IPs is the multi-hop client address list | 多跳的客户端地址列表。
	IPs string `json:"ips"`
	// Host is the request Host header | 请求的 Host 头。
	Host string `json:"host"`
	// ClientIP is the resolved client address | 解析出的客户端地址。
	ClientIP string `json:"client_ip"`
	// URL is the full request URL | 完整请求 URL。
	URL string `json:"url"`
	// UA is the User-Agent request header | User-Agent 请求头。
	UA string `json:"ua"`

	// Method is the HTTP method | HTTP 方法。
	Method string `json:"method"`
	// Path is the request path | 请求路径。
	Path string `json:"path"`
	// ReqBody is the captured request body (truncated) | 捕获的请求体（已截断）。
	ReqBody string `json:"req_body"`
	// Duration is the request cost | 请求耗时。
	Duration string `json:"duration"`
	// StatusCode is the response status code | 响应状态码。
	StatusCode int `json:"status_code"`
	// RespBody is the captured response body (truncated) | 捕获的响应体（已截断）。
	RespBody string `json:"resp_body"`
}

type responseWriter struct {
	al *AccessLog
	gin.ResponseWriter
	maxLength int64
}

// WriteHeader records the status, then delegates | WriteHeader 记录状态码后委托底层写入.
func (r responseWriter) WriteHeader(statusCode int) {
	r.al.StatusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write captures the (truncated) body, then delegates | Write 捕获（截断后的）响应体后委托底层写入.
func (r responseWriter) Write(data []byte) (int, error) {
	curLen := int64(len(data))
	if curLen >= r.maxLength {
		data = data[:r.maxLength]
	}
	r.al.RespBody = string(data)
	return r.ResponseWriter.Write(data)
}

// WriteString captures the (truncated) body, then delegates | WriteString 捕获（截断后的）响应体后委托底层写入.
func (r responseWriter) WriteString(data string) (int, error) {
	curLen := int64(len(data))
	if curLen >= r.maxLength {
		data = data[:r.maxLength]
	}
	r.al.RespBody = data
	return r.ResponseWriter.WriteString(data)
}
