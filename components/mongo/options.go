package mongo

import (
	"time"

	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	Uri            string
	DatabaseName   string
	CollectionName string

	*options.ClientOptions

	interceptors []Interceptor

	Debug bool
}

type Option interface {
	apply(*Config)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Config)

func (f OptionFunc) apply(o *Config) {
	f(o)
}

// DefaultOptions .
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

// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Config {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Config) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

func WithURI(uri string) Option {
	return OptionFunc(func(config *Config) {
		config.Uri = uri
	})
}

func WithDebug(debug bool) Option {
	return OptionFunc(func(config *Config) {
		config.Debug = debug
	})
}

func WithInterceptors(interceptors []Interceptor) Option {
	return OptionFunc(func(config *Config) {
		config.interceptors = interceptors
	})
}

func WithInterceptor(interceptors ...Interceptor) Option {
	return OptionFunc(func(c *Config) {
		if c.interceptors == nil {
			c.interceptors = make([]Interceptor, 0)
		}
		c.interceptors = append(c.interceptors, interceptors...)
	})
}

func WithDatabaseName(dbname string) Option {
	return OptionFunc(func(config *Config) {
		config.DatabaseName = dbname
	})
}

func WithCollectionName(collname string) Option {
	return OptionFunc(func(config *Config) {
		config.CollectionName = collname
	})
}

// WithMongoConfig 设置所有配置
func WithMongoConfig(fn func(options *Config)) Option {
	return OptionFunc(func(config *Config) {
		fn(config)
	})
}

// WithClientOptions 表示自行配置ClientOptions的配置信息
func WithClientOptions(options *options.ClientOptions) Option {
	return OptionFunc(func(config *Config) {
		config.ClientOptions = options
	})
}
