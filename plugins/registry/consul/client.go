package consul

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// client is a minimal Consul HTTP API client | client 是一个极简的 Consul
// HTTP API 客户端.
type client struct {
	base       string
	http       *http.Client
	token      string
	datacenter string
	timeout    time.Duration
}

func newClient(o Options) *client {
	return &client{
		base:       o.address,
		http:       o.httpClient,
		token:      o.token,
		datacenter: o.datacenter,
		timeout:    o.requestTimeout,
	}
}

// statusError is a non-2xx answer from Consul | statusError 表示 Consul 返回
// 的非 2xx 应答.
type statusError struct {
	// Method is the HTTP method used | 使用的 HTTP 方法.
	Method string
	// Path is the request path | 请求路径.
	Path string
	// Code is the HTTP status code | HTTP 状态码.
	Code int
	// Body is a truncated response body | 截断后的响应体.
	Body string
}

// Error implements the error interface | 实现 error 接口.
func (e *statusError) Error() string {
	return fmt.Sprintf("consul: %s %s: %d %s", e.Method, e.Path, e.Code, e.Body)
}

func isNotFound(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

// isFatal reports an error that retrying cannot fix: a client error such as
// a missing ACL permission. Timeouts and rate limits are not fatal.
//
// isFatal 报告重试无法挽回的错误：即缺 ACL 权限这类客户端错误。
// 超时与限流不算致命。
func isFatal(err error) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return se.Code >= 400 && se.Code < 500
}

// do sends one request and returns the X-Consul-Index of the answer. A
// non-blocking request is bounded by the client timeout; a blocking query is
// bounded by the server wait plus jitter and a request timeout margin.
//
// do 发送一个请求并返回应答的 X-Consul-Index。非阻塞请求受客户端超时约束；
// 阻塞查询受服务端 wait 加抖动、再留一次请求超时余量约束。
func (c *client) do(ctx context.Context, method, path string, query url.Values, in, out any, blocking bool) (uint64, error) {
	timeout := c.timeout
	if blocking {
		wait, _ := time.ParseDuration(query.Get("wait"))
		// Consul may add up to wait/16 of jitter.
		// Consul 最多可能附加 wait/16 的抖动。
		timeout += wait + wait/16
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if query == nil {
		query = url.Values{}
	}
	if c.datacenter != "" {
		query.Set("dc", c.datacenter)
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, fmt.Errorf("consul: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return 0, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("X-Consul-Token", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, &statusError{Method: method, Path: path, Code: resp.StatusCode, Body: string(bytes.TrimSpace(b))}
	}
	var index uint64
	if v := resp.Header.Get("X-Consul-Index"); v != "" {
		index, _ = strconv.ParseUint(v, 10, 64)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return index, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return 0, fmt.Errorf("consul: decode %s response: %w", path, err)
	}
	return index, nil
}
