// Package mail defines a provider-neutral email sending abstraction | 定义 Provider 无关的邮件发送抽象.
//
// # Notes | 说明
//
// The core declares only Message, Sender, Receipt and a unified error
// contract; it depends on no third-party SDK. Providers (Resend, Amazon SES
// v2, ...) implement Sender in separate modules — the application creates the
// official SDK client and injects the adapter. Send success only means the
// provider accepted the request (accepted != delivered); bounces and delivery
// receipts arrive via webhooks, not through this API.
// 核心只定义 Message、Sender、Receipt 与统一错误契约，不依赖任何第三方
// SDK。各 Provider（Resend、Amazon SES v2……）在独立 module 中实现 Sender，
// 由应用创建官方 SDK Client 并注入适配器。Send 成功仅表示 Provider 已接受
// 请求（accepted != delivered）；退信与投递回执走 Webhook，不经本 API。
//
// # Errors | 错误
//
// - ErrInvalidMessage: common validation failed, no request was sent | 公共校验失败，未发起请求
// - context.Canceled or DeadlineExceeded: cancelled before the client call | 调用 Client 前已取消，原样返回
// - *DeliveryError: failed after the client call; Unwrap keeps the SDK error, IsTransient and Outcome guide retries | 调用 Client 后失败；保留原始 SDK 错误，由 IsTransient 与 Outcome 指导重试
//
// Automatic retry requires both IsTransient(err) and !MayHaveBeenAccepted(err).
// 自动重试须同时满足 IsTransient(err) 与 !MayHaveBeenAccepted(err)。
//
// # Concurrency | 并发
//
// Sender implementations are immutable and safe for concurrent use; the caller
// must not mutate a Message's maps, slices or attachments during Send.
// Sender 实现不可变、可并发使用；Send 进行期间调用方不得修改 Message 的
// map、slice 或附件。
package mail
