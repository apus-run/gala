// Package ses implements mail.Sender on the AWS SDK for Go v2 SES API v2
// | 基于 AWS SDK for Go v2 的 SES API v2 实现 mail.Sender.
//
// # Notes | 说明
//
// The adapter loads no AWS configuration: the app creates the official
// client and injects it, keeping the credential chain, Region, Retryer,
// proxy and tracing under its own control.
// 适配器不加载 AWS 配置：应用创建官方 Client 并注入，
// 凭据链、Region、Retryer、代理与 Tracing 由应用自己持有。
//
// Messages are built as SES Simple content (subject, bodies, headers,
// attachments, inline CID); the adapter never assembles raw MIME.
// 消息用 SES Simple Content 构造（主题、正文、Headers、附件、Inline CID），
// 不自行拼装 Raw MIME。
//
// Error classification follows the core mail contract: context errors before
// the client call are returned as-is; because the app owns the Retryer and
// the final error exposes no attempt history, every post-call failure is
// SendOutcomeUnknown.
// 错误分类遵循核心契约：调用 Client 前的 context 错误原样返回；
// 由于 Retryer 由应用注入且最终错误不暴露尝试历史，
// 调用后的所有失败均为 SendOutcomeUnknown。
//
// # Features | 功能
//
// - Optional SES configuration set via WithConfigurationSet | 通过 WithConfigurationSet 可选设置 SES Configuration Set
// - In-process message budget (DefaultMaxMessageBytes, 10 MiB), independent of the SES 40 MiB encoded limit | 进程内消息预算（DefaultMaxMessageBytes，10 MiB），独立于 SES 编码后 40 MiB 硬限制
//
// # Example | 示例
//
//	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("ap-southeast-1"))
//	if err != nil { return err }
//	sender, err := ses.New(sesv2.NewFromConfig(awsCfg), ses.WithConfigurationSet("production"))
package ses
