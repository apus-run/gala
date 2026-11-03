// Package resend implements mail.Sender on the official resend-go/v3 SDK
// | 基于官方 resend-go/v3 SDK 实现 mail.Sender.
//
// # Notes | 说明
//
// The adapter builds no network stack: the app constructs the official SDK
// client and injects its Emails service, so API keys, HTTP behavior and
// observability stay outside this package.
// 适配器不自建网络栈：应用先构造官方 SDK Client，注入其 Emails 服务，
// API Key、HTTP 行为与观测配置都留在本包之外。
//
// Errors follow the core mail contract: context errors surfaced before the
// client call are returned as-is; failures after the call are wrapped as
// *mail.DeliveryError, always SendOutcomeUnknown except rate limiting.
// 错误遵循 mail 核心契约：调用 Client 前的 context 错误原样返回；
// 调用后的失败包装为 *mail.DeliveryError，除限流外一律 SendOutcomeUnknown。
//
// # Features | 功能
//
// - Idempotent send via SendOptions.IdempotencyKey, 1-256 chars, kept 24h by Resend | 通过 SendOptions.IdempotencyKey 幂等发送，1～256 字符，Resend 保留 24 小时
// - NewFromAPIKey convenience constructor reusing the SDK client defaults | NewFromAPIKey 便捷构造，沿用 SDK 默认 Client 行为
// - In-process message budget (DefaultMaxMessageBytes, 10 MiB) enforced before serialization | 序列化前生效的进程内消息预算（DefaultMaxMessageBytes，10 MiB）
//
// # Example | 示例
//
//	sdkClient := resendgo.NewClient(apiKey)
//	sender, err := resend.New(sdkClient.Emails)
//	// ...
//	receipt, err := sender.Send(ctx, msg)
package resend
