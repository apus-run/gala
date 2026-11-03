package ses

// DefaultMaxMessageBytes is the default budget over the approximate raw input.
// It is an in-process safety cap independent of the SES 40 MiB encoded hard
// limit; both are enforced before any copy or serialization.
// DefaultMaxMessageBytes 是近似原始输入的默认消息预算。
// 它是独立于 SES 40 MiB 编码后硬限制的进程内安全上限，二者都在
// 任何复制与序列化之前生效。
const DefaultMaxMessageBytes int64 = 10 << 20 // 10 MiB

// Option configures a Sender | Option 配置 Sender。
type Option interface {
	apply(*Sender)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option。
type OptionFunc func(*Sender)

func (f OptionFunc) apply(o *Sender) {
	f(o)
}

// WithConfigurationSet sets the SES configuration set used for sending.
// An empty string means none; a non-empty name must be at most 64 characters
// of ASCII letters, digits, underscores and hyphens only.
// WithConfigurationSet 设置发送使用的 SES Configuration Set。
// 空字符串表示不使用；非空名称必须不超过 64 个字符，且只能包含
// ASCII 字母、数字、下划线与短横线。
func WithConfigurationSet(name string) Option {
	return OptionFunc(func(s *Sender) { s.configurationSet = name })
}

// WithMaxMessageBytes adjusts the approximate raw message budget.
// Raising the budget cannot bypass the SES 40 MiB encoded hard limit; the
// application must also bound concurrent sends against this budget.
// WithMaxMessageBytes 调整近似原始消息预算。提高预算不能绕过
// SES 40 MiB 编码后硬限制；应用还必须按该预算限制并发发送数。
func WithMaxMessageBytes(n int64) Option {
	return OptionFunc(func(s *Sender) { s.maxMessageBytes = n })
}
