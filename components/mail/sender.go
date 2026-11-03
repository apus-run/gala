package mail

import "context"

// Sender 发送一封邮件。实现必须可并发使用。
//
// 返回的 Receipt 仅表示 Provider 已接受请求，不表示最终投递成功。
// Sender sends one email; implementations must be safe for concurrent use.
//
// The returned Receipt only means the provider accepted the request,
// not that delivery ultimately succeeded.
type Sender interface {
	// Send 提交一封邮件 | Send submits one email.
	Send(ctx context.Context, msg Message) (Receipt, error)
}

// SenderFunc 让函数直接充当 Sender，便于测试、装饰器与简单适配。
// SenderFunc lets a function act as a Sender for tests, decorators and simple adapters.
type SenderFunc func(context.Context, Message) (Receipt, error)

// Send 实现 Sender。
// Send implements Sender.
func (f SenderFunc) Send(ctx context.Context, msg Message) (Receipt, error) {
	return f(ctx, msg)
}
