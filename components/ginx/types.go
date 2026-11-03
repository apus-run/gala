package ginx

import (
	"context"
	"errors"
	"net/http/httputil"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// CodeOK means a successful response | 成功响应码。
	CodeOK = 0
	// CodeErr means a failure response | 失败响应码。
	CodeErr = 1
)

const requestIdFieldKey = "REQUEST_ID"

// AcceptLanguageHeaderName represents the header name of accept language
// AcceptLanguageHeaderName 表示 Accept-Language 请求头的名称。
const AcceptLanguageHeaderName = "Accept-Language"

// ClientTimezoneOffsetHeaderName represents the header name of client timezone offset
// ClientTimezoneOffsetHeaderName 表示客户端时区偏移请求头的名称。
const ClientTimezoneOffsetHeaderName = "X-Timezone-Offset"

// Handler is implemented by modules that register their own routes
// | Handler 由需要自行注册路由的模块实现。
type Handler interface {
	PrivateRoutes(r gin.IRoutes)
	PublicRoutes(r gin.IRoutes)
}

// Context a wrapper of gin.Context | Context 是 gin.Context 的包装。
type Context struct {
	*gin.Context
}

// HandlerFunc defines the handler to wrap gin.Context
// HandlerFunc 定义包装 gin.Context 的处理函数。
type HandlerFunc func(*Context)

// ProxyHandlerFunc represents the reverse proxy handler function
// ProxyHandlerFunc 表示反向代理处理函数。
type ProxyHandlerFunc func(*Context) (*httputil.ReverseProxy, error)

// Result defines HTTP JSON response | Result 定义 HTTP JSON 响应结构。
type Result struct {
	Code    int      `json:"code"`
	Msg     string   `json:"msg"`
	Data    any      `json:"data"`
	Details []string `json:"details,omitempty"`
}

// Option is config option | Option 是配置选项。
type Option func(*Options) error

// Options holds the server configuration | Options 存放服务器配置。
type Options struct {
	mode         string // dev or prod | dev 或 prod
	host         string
	port         string
	maxPingCount int

	// Before and After funcs | 启动前后钩子
	beforeStart []func(context.Context) error
	afterStart  []func(context.Context) error
	afterStop   []func(context.Context) error
}

// DefaultOptions returns a new Options holding the package defaults.
// DefaultOptions 返回带包默认值的新 Options。
func DefaultOptions() *Options {
	return &Options{
		mode:         "dev",
		host:         "localhost",
		port:         "8080",
		maxPingCount: 5,
	}
}

// NewOptions builds Options from the defaults, then applies opts in order.
// It returns the first option error, so an invalid option is never silently dropped.
// NewOptions 先基于默认值构建 Options，再按序应用 opts；
// 返回第一个选项错误，非法选项不会被静默丢弃。
func NewOptions(opts ...Option) (*Options, error) {
	o := DefaultOptions()
	if err := o.Apply(opts...); err != nil {
		return nil, err
	}
	return o, nil
}

// Apply applies opts in place in order; it stops and returns the first error,
// so later options are not applied | Apply 按顺序原地应用选项；
// 遇到第一个错误即停止并返回该错误，之后的选项不再应用。
func (o *Options) Apply(opts ...Option) error {
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return err
		}
	}
	return nil
}

// WithMode sets the run mode, which must be dev or prod
// | WithMode 设置运行模式，只允许 dev 或 prod。
func WithMode(mode string) Option {
	return func(o *Options) error {
		if strings.ToLower(mode) != "dev" && strings.ToLower(mode) != "prod" {
			return errors.New("mode must be dev or prod")
		}
		o.mode = mode
		return nil
	}
}

// WithAddr sets the listen host, which must not be empty
// | WithAddr 设置监听主机，不能为空。
func WithAddr(host string) Option {
	return func(o *Options) error {
		if host == "" {
			return errors.New("host can not be empty")
		}
		o.host = host
		return nil
	}
}

// WithPort sets the listen port, which must not be empty
// | WithPort 设置监听端口，不能为空。
func WithPort(port string) Option {
	return func(o *Options) error {
		if port == "" {
			return errors.New("port can not be empty")
		}
		o.port = port
		return nil
	}
}

// WithMaxPingCount sets the max ping count, which must be greater than 0
// | WithMaxPingCount 设置最大 ping 次数，必须大于 0。
func WithMaxPingCount(maxPingCount int) Option {
	return func(o *Options) error {
		if maxPingCount <= 0 {
			return errors.New("maxPingCount must be greater than 0")
		}
		o.maxPingCount = maxPingCount
		return nil
	}
}

// Before and Afters | 启动前后钩子

// BeforeStart run funcs before app starts | BeforeStart 在应用启动前执行函数。
func BeforeStart(fn func(context.Context) error) Option {
	return func(o *Options) error {
		if fn == nil {
			return errors.New("beforeStart func can not be nil")
		}
		o.beforeStart = append(o.beforeStart, fn)
		return nil
	}
}

// AfterStart run funcs after app starts | AfterStart 在应用启动后执行函数。
func AfterStart(fn func(context.Context) error) Option {
	return func(o *Options) error {
		if fn == nil {
			return errors.New("afterStart func can not be nil")
		}
		o.afterStart = append(o.afterStart, fn)
		return nil
	}
}

// AfterStop run funcs after app stops | AfterStop 在应用停止后执行函数。
func AfterStop(fn func(context.Context) error) Option {
	return func(o *Options) error {
		if fn == nil {
			return errors.New("afterStop func can not be nil")
		}
		o.afterStop = append(o.afterStop, fn)
		return nil
	}
}
