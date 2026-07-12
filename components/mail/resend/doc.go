// Package resend 基于官方 resend-go/v3 SDK 实现 mail.Sender。
//
// 适配器不创建网络栈：应用先构造官方 Client，再注入其 Emails 服务：
//
//	sdkClient := resendgo.NewClient(apiKey)
//	sender, err := resend.New(sdkClient.Emails)
//
// 普通业务只依赖 mail.Sender；需要 Resend 幂等键的基础设施代码
// 显式使用 *Sender.SendWithOptions。
//
// 错误分类遵循核心契约：调用 Client 前的 Context 错误原样返回；
// 调用 Client 后的失败包装为 *mail.DeliveryError，除限流
// （已知未接受）外一律 SendOutcomeUnknown。
package resend
