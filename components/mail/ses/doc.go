// Package ses 基于 AWS SDK for Go v2 的 SES API v2 实现 mail.Sender。
//
// 适配器不加载 AWS 配置：应用创建官方 Client 并注入，
// 继续拥有凭据链、Region、Retryer、代理与 Tracing：
//
//	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("ap-southeast-1"))
//	sesClient := sesv2.NewFromConfig(awsCfg)
//	sender, err := ses.New(sesClient, ses.WithConfigurationSet("production"))
//
// 消息使用 SES Simple Content 构造（Subject、正文、Headers、附件、
// Inline CID），不自行拼装 Raw MIME。
//
// 错误分类遵循核心契约：调用 Client 前的 Context 错误原样返回；
// 由于 AWS Client 的 Retryer 由应用注入且最终错误不暴露完整尝试历史，
// 调用 Client 后的所有失败均为 SendOutcomeUnknown。
package ses
