// Package mail 提供 Provider 无关的邮件发送抽象。
//
// 核心只定义 Message、Sender、Receipt 与统一错误，不依赖任何第三方 SDK。
// 具体 Provider（Resend、Amazon SES v2 等）在独立 Go module 中实现 Sender，
// 由应用在启动层创建官方 SDK Client 并显式注入适配器。
//
// # 成功语义
//
// Send 成功仅表示 Provider 已接受请求（accepted != delivered），
// 不表示最终投递成功。退信、投诉、投递回执应由应用通过
// Webhook、SNS 或 EventBridge 处理。
//
// # 错误契约
//
//   - 公共校验失败：errors.Is(err, ErrInvalidMessage)。
//   - 调用 Client 前 Context 已取消：原样返回 context.Canceled 或
//     context.DeadlineExceeded，不调用 Client。
//   - 调用 Client 后失败：返回 *DeliveryError，Unwrap 保留原始 SDK 错误。
//     IsTransient 表示同类故障之后可能恢复；Outcome 表示 Provider 是否
//     可能已接受请求，零值 SendOutcomeUnknown 是保守默认。
//
// 通用自动重试候选必须同时满足 IsTransient(err) 与
// !MayHaveBeenAccepted(err)；OutcomeUnknown 的错误只有在错误为瞬态，
// 且 Provider 幂等保护有效或业务明确接受至少一次语义时才可重试。
//
// # 并发
//
// Sender 构造后不可变，可并发使用。Send 不修改调用方的 Message，
// 但调用方不得在 Send 进行期间并发修改同一 Message 的
// Map、Slice 或附件内容。
package mail
