package mail

import "context"

// Sender 发送一封邮件。实现必须可并发使用。
//
// 返回的 Receipt 仅表示 Provider 已接受请求，不表示最终投递成功。
type Sender interface {
	Send(ctx context.Context, msg Message) (Receipt, error)
}

// SenderFunc 让函数直接充当 Sender，便于测试、装饰器与简单适配。
type SenderFunc func(context.Context, Message) (Receipt, error)

// Send 实现 Sender。
func (f SenderFunc) Send(ctx context.Context, msg Message) (Receipt, error) {
	return f(ctx, msg)
}
