package consul

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAddress         = "http://127.0.0.1:8500"
	defaultTTL             = 15 * time.Second
	defaultDeregisterAfter = time.Minute
	defaultWait            = 30 * time.Second
	defaultRequestTimeout  = 10 * time.Second
	defaultBufferSize      = 64
	defaultRetryMin        = 500 * time.Millisecond
	defaultRetryMax        = 10 * time.Second
	defaultMinPoll         = 200 * time.Millisecond
)

// Options holds the shared configuration for Registry and Discovery | 持有 Registry 与 Discovery 的共享配置.
type Options struct {
	address         string
	token           string
	datacenter      string
	ttl             time.Duration
	deregisterAfter time.Duration
	wait            time.Duration
	requestTimeout  time.Duration
	bufferSize      int
	httpClient      *http.Client
	logger          *slog.Logger

	// Unexported so tests can shorten them.
	// 未导出，便于测试缩短它们。
	checkInterval time.Duration // 0 means ttl/3 | 0 表示 ttl/3
	retryMin      time.Duration // backoff after a failed poll | 轮询失败后的退避起点
	retryMax      time.Duration
	minPoll       time.Duration // floor between polls that return nothing new | 无新数据轮询之间的最小间隔
}

// Option configures a Registry or a Discovery | Option 配置 Registry 或 Discovery.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// WithAddress sets the Consul agent address, "host:port" or a full URL.
// Default: $CONSUL_HTTP_ADDR, else http://127.0.0.1:8500.
// WithAddress 设置 Consul agent 地址，"host:port" 或完整 URL。
// 默认：$CONSUL_HTTP_ADDR，否则 http://127.0.0.1:8500.
func WithAddress(addr string) Option { return OptionFunc(func(o *Options) { o.address = addr }) }

// WithToken sets the ACL token. Default: $CONSUL_HTTP_TOKEN.
// WithToken 设置 ACL token。默认：$CONSUL_HTTP_TOKEN.
func WithToken(token string) Option { return OptionFunc(func(o *Options) { o.token = token }) }

// WithDatacenter sets the datacenter to query. Default: the agent's.
// WithDatacenter 设置要查询的 datacenter。默认：agent 所在的.
func WithDatacenter(dc string) Option { return OptionFunc(func(o *Options) { o.datacenter = dc }) }

// WithTTL sets the TTL of the health check. An instance becomes critical
// this long after its process stops passing the check. Default: 15s.
// WithTTL 设置健康检查的 TTL。进程停止续约这么久之后实例转为 critical。
// 默认：15s.
func WithTTL(ttl time.Duration) Option { return OptionFunc(func(o *Options) { o.ttl = ttl }) }

// WithDeregisterCriticalAfter makes Consul remove an instance whose check
// has been critical this long. Zero disables it. Default: 1m.
// WithDeregisterCriticalAfter 让 Consul 移除 critical 持续这么久的实例。
// 零值关闭该行为。默认：1m.
func WithDeregisterCriticalAfter(d time.Duration) Option {
	return OptionFunc(func(o *Options) { o.deregisterAfter = d })
}

// WithWaitTime sets how long a blocking query waits for a change before
// returning. Default: 30s.
// WithWaitTime 设置阻塞查询等待变更的最长时间。默认：30s.
func WithWaitTime(d time.Duration) Option { return OptionFunc(func(o *Options) { o.wait = d }) }

// WithRequestTimeout bounds every non-blocking request. Default: 10s.
// WithRequestTimeout 限制每个非阻塞请求的超时。默认：10s.
func WithRequestTimeout(d time.Duration) Option {
	return OptionFunc(func(o *Options) { o.requestTimeout = d })
}

// WithBufferSize sets how many events a subscription can queue beyond its
// initial replay before it is closed as a slow consumer. Default: 64.
// WithBufferSize 设置订阅在初始重放之外还能排队的事件数，超过即作为慢消费者
// 被关闭。默认：64.
func WithBufferSize(n int) Option { return OptionFunc(func(o *Options) { o.bufferSize = n }) }

// WithHTTPClient sets the HTTP client, e.g. for TLS. Its Timeout must be
// longer than WithWaitTime, or blocking queries will be cut short.
// WithHTTPClient 设置 HTTP client，例如用于 TLS。其 Timeout 必须长于
// WithWaitTime，否则阻塞查询会被中途掐断.
func WithHTTPClient(c *http.Client) Option {
	return OptionFunc(func(o *Options) {
		if c != nil {
			o.httpClient = c
		}
	})
}

// WithLogger sets the logger. Default: slog.Default().
// WithLogger 设置日志器。默认：slog.Default().
func WithLogger(l *slog.Logger) Option {
	return OptionFunc(func(o *Options) {
		if l != nil {
			o.logger = l
		}
	})
}

// DefaultOptions returns the package defaults. address and token are read from
// CONSUL_HTTP_ADDR and CONSUL_HTTP_TOKEN, so the result depends on the environment.
// DefaultOptions 返回包默认值。address 与 token 取自 CONSUL_HTTP_ADDR 和
// CONSUL_HTTP_TOKEN，因此结果依赖环境.
func DefaultOptions() Options {
	return Options{
		address:         os.Getenv("CONSUL_HTTP_ADDR"),
		token:           os.Getenv("CONSUL_HTTP_TOKEN"),
		ttl:             defaultTTL,
		deregisterAfter: defaultDeregisterAfter,
		wait:            defaultWait,
		requestTimeout:  defaultRequestTimeout,
		bufferSize:      defaultBufferSize,
		httpClient:      &http.Client{},
		logger:          slog.Default(),
		retryMin:        defaultRetryMin,
		retryMax:        defaultRetryMax,
		minPoll:         defaultMinPoll,
	}
}

func newOptions(opts []Option) (Options, error) {
	o := DefaultOptions()
	for _, opt := range opts {
		opt.apply(&o)
	}

	addr, err := normalizeAddress(o.address)
	if err != nil {
		return Options{}, err
	}
	o.address = addr

	switch {
	case o.ttl <= 0:
		o.ttl = defaultTTL
	case o.ttl < time.Second:
		o.ttl = time.Second
	}
	if o.deregisterAfter < 0 {
		o.deregisterAfter = 0
	}
	if o.wait <= 0 {
		o.wait = defaultWait
	}
	if o.requestTimeout <= 0 {
		o.requestTimeout = defaultRequestTimeout
	}
	if o.bufferSize <= 0 {
		o.bufferSize = defaultBufferSize
	}
	return o, nil
}

func normalizeAddress(addr string) (string, error) {
	if addr == "" {
		addr = defaultAddress
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("consul registry: invalid address %q", addr)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// interval is how often the TTL check is passed.
// interval 是 TTL 检查续约的频率.
func (o *Options) interval() time.Duration {
	if o.checkInterval > 0 {
		return o.checkInterval
	}
	return o.ttl / 3
}
