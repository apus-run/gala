package mongo

import (
	"time"

	"go.mongodb.org/mongo-driver/mongo/options"
)

// Config holds the settings for connecting to MongoDB | Config 持连接 MongoDB 的配置。
type Config struct {
	// Uri is the MongoDB connection URI | MongoDB 连接 URI
	Uri string
	// DatabaseName selects the default database | 默认数据库名
	DatabaseName string
	// CollectionName selects the default collection | 默认集合名
	CollectionName string

	*options.ClientOptions

	interceptors []Interceptor

	// Debug enables command logging on the wrappers | 是否开启包装器的命令日志
	Debug bool
}

// Option configures a Config | Option 配置 Config。
type Option interface {
	apply(*Config)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option。
type OptionFunc func(*Config)

func (f OptionFunc) apply(o *Config) {
	f(o)
}

// DefaultOptions returns the package defaults: 30s connect timeout, 3m max
// idle time, pool between 20 and 300 connections.
// DefaultOptions 返回包默认配置：连接超时 30 秒、最大空闲 3 分钟、
// 连接池 20～300。
func DefaultOptions() *Config {
	connectTimeout := 30 * time.Second
	maxConnIdleTime := 3 * time.Minute
	minPoolSize := uint64(20)
	maxPoolSize := uint64(300)

	return &Config{
		ClientOptions: &options.ClientOptions{
			ConnectTimeout:  &connectTimeout,
			MaxConnIdleTime: &maxConnIdleTime,
			MinPoolSize:     &minPoolSize,
			MaxPoolSize:     &maxPoolSize,
		},
	}
}

// NewOptions starts from DefaultOptions and applies opts in order
// | NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Config {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place, in order, without resetting defaults or
// validating.
// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Config) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithURI sets the connection URI | WithURI 设置连接 URI。
func WithURI(uri string) Option {
	return OptionFunc(func(config *Config) {
		config.Uri = uri
	})
}

// WithDebug toggles command logging on the wrappers | WithDebug 开关包装器的命令日志。
func WithDebug(debug bool) Option {
	return OptionFunc(func(config *Config) {
		config.Debug = debug
	})
}

// WithInterceptors replaces the interceptor chain | WithInterceptors 整体替换拦截器链。
func WithInterceptors(interceptors []Interceptor) Option {
	return OptionFunc(func(config *Config) {
		config.interceptors = interceptors
	})
}

// WithInterceptor appends interceptors to the chain | WithInterceptor 追加拦截器。
func WithInterceptor(interceptors ...Interceptor) Option {
	return OptionFunc(func(c *Config) {
		if c.interceptors == nil {
			c.interceptors = make([]Interceptor, 0)
		}
		c.interceptors = append(c.interceptors, interceptors...)
	})
}

// WithDatabaseName sets the default database name | WithDatabaseName 设置默认数据库名。
func WithDatabaseName(dbname string) Option {
	return OptionFunc(func(config *Config) {
		config.DatabaseName = dbname
	})
}

// WithCollectionName sets the default collection name | WithCollectionName 设置默认集合名。
func WithCollectionName(collname string) Option {
	return OptionFunc(func(config *Config) {
		config.CollectionName = collname
	})
}

// WithMongoConfig sets the whole config through fn | WithMongoConfig 通过 fn 设置所有配置。
func WithMongoConfig(fn func(options *Config)) Option {
	return OptionFunc(func(config *Config) {
		fn(config)
	})
}

// WithClientOptions replaces the underlying driver ClientOptions
// | WithClientOptions 自行配置底层驱动的 ClientOptions。
func WithClientOptions(options *options.ClientOptions) Option {
	return OptionFunc(func(config *Config) {
		config.ClientOptions = options
	})
}
