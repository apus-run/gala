package casbin

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// MatchAllRule requires every item. An empty list skips authorization.
	// MatchAllRule 要求所有条目都命中。空列表跳过授权。
	MatchAllRule ValidationRule = iota
	// AtLeastOneRule requires one matching item. An empty list also skips authorization.
	// AtLeastOneRule 要求至少一条命中。空列表同样跳过授权。
	AtLeastOneRule
)

type (
	// ValidationRule controls how a list of permissions or roles is matched.
	// ValidationRule 控制权限或角色列表的匹配方式。
	ValidationRule int

	// PermissionParserFunc turns a permission into Casbin request fields after the
	// subject. The default splits "blog:create" into object and action. It runs on
	// each checked item and must be concurrency-safe if handlers share it.
	// PermissionParserFunc 把权限拆成 subject 之后的 Casbin 请求字段。默认把
	// "blog:create" 拆成 object 与 action。它在每个被检查条目上运行；
	// 多 handler 共享时必须并发安全。
	PermissionParserFunc func(string) []string

	// Option configures a permission or role handler.
	// Option 配置权限或角色 handler。
	Option interface {
		apply(*Options)
	}

	// OptionFunc adapts a configuration function to Option.
	// OptionFunc 把配置函数适配为 Option。
	OptionFunc func(*Options)

	// Options configure one generated handler, independently of the Builder.
	// Options 为单个生成的 handler 提供配置，独立于 Builder。
	Options struct {
		validationRule   ValidationRule
		permissionParser PermissionParserFunc
	}
)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// NewOptions creates default options and applies opts in order.
// NewOptions 创建默认配置并按序应用 opts。
func NewOptions(opts ...Option) *Options {
	options := DefaultOptions()
	options.Apply(opts...)
	return options
}

// DefaultOptions returns an independent default configuration.
// DefaultOptions 返回独立的默认配置。
func DefaultOptions() *Options {
	return &Options{
		validationRule:   MatchAllRule,
		permissionParser: PermissionParserWithSeparator(":"),
	}
}

// Apply applies opts in order without resetting existing configuration or validating.
// The receiver and options must be valid. Apply must not run concurrently with
// other reads or writes to the same Options.
// Apply 按序应用 opts，不重置已有配置也不校验。接收者与选项必须合法；
// Apply 不得与其他对同一 Options 的读写并发执行。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Validate checks the final configuration without modifying it.
// Validate 校验最终配置且不修改它。
func (o *Options) Validate() error {
	if o.validationRule != MatchAllRule && o.validationRule != AtLeastOneRule {
		return fmt.Errorf("casbin middleware: invalid ValidationRule %d", o.validationRule)
	}
	if o.permissionParser == nil {
		return errors.New("casbin middleware: nil PermissionParser")
	}
	return nil
}

// Apply creates default options and applies opts in order.
// Deprecated: use NewOptions to create options or (*Options).Apply to update them.
// Apply 创建默认配置并按序应用 opts。
// 已废弃：创建配置请用 NewOptions，更新配置请用 (*Options).Apply。
func Apply(opts ...Option) *Options {
	return NewOptions(opts...)
}

// WithValidationRule selects all or any matching for one handler.
// WithValidationRule 为单个 handler 选择全部命中或任一命中。
func WithValidationRule(rule ValidationRule) Option {
	return OptionFunc(func(o *Options) { o.validationRule = rule })
}

// WithPermissionParser sets parsing for permission checks; roles do not invoke it.
// WithPermissionParser 设置权限检查的解析函数；角色检查不会调用它。
func WithPermissionParser(parser PermissionParserFunc) Option {
	return OptionFunc(func(o *Options) { o.permissionParser = parser })
}

// PermissionParserWithSeparator uses strings.Split, including empty fields.
// PermissionParserWithSeparator 使用 strings.Split（含空字段）。
func PermissionParserWithSeparator(separator string) PermissionParserFunc {
	return func(permission string) []string { return strings.Split(permission, separator) }
}
