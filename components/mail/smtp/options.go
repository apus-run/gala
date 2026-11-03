package smtp

// DefaultMaxMessageBytes is the default budget over the approximate raw input.
// It is an in-process safety cap enforced before any buffering or connection
// setup; the SMTP server's own message size limit (the EHLO SIZE extension)
// is enforced separately by the server.
// DefaultMaxMessageBytes 是近似原始输入的默认消息预算。
// 它是进程内安全上限，在任何缓冲与建连之前生效；SMTP 服务器
// 自身的消息大小限制（EHLO SIZE 扩展）由服务器另行执行。
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

// WithMaxMessageBytes adjusts the approximate raw message budget.
// Raising the budget cannot bypass the SMTP server's hard limits; the
// application must also bound concurrent sends against this budget.
// WithMaxMessageBytes 调整近似原始消息预算。提高预算不能绕过
// SMTP 服务器的硬限制；应用还必须按该预算限制并发发送数。
func WithMaxMessageBytes(n int64) Option {
	return OptionFunc(func(s *Sender) { s.maxMessageBytes = n })
}

// WithMessageIDGenerator injects a Message-ID generator. The returned value
// carries no angle brackets and must be printable ASCII without '<', '>', ','
// or ';' (a "<unique>@<domain>" shape is recommended). Use it to customize the
// ID domain or make tests deterministic; passing nil restores go-mail's
// default random generation.
// WithMessageIDGenerator 注入 Message-ID 生成器，返回值不含尖括号，
// 须为可打印 ASCII 且不含 '<'、'>'、','、';'（建议 "<unique>@<domain>"
// 形式）。用于自定义 ID 域名或测试确定性；传 nil 恢复默认的
// go-mail 随机生成。
func WithMessageIDGenerator(gen func() string) Option {
	return OptionFunc(func(s *Sender) { s.generateMessageID = gen })
}
