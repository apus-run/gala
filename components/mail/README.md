# mail

Provider 无关的邮件发送组件。核心模块零第三方依赖，只定义 `Message`、`Sender`、`Receipt` 与统一错误；Resend 与 Amazon SES v2 适配器在独立 Go module 中实现。

```text
核心：github.com/apus-run/gala/components/mail
Resend：github.com/apus-run/gala/components/mail/resend
SES：github.com/apus-run/gala/components/mail/ses
SMTP：github.com/apus-run/gala/components/mail/smtp
```

仅使用某个 Provider 时，只引入该 Provider 的依赖；业务代码只依赖 `mail.Sender`，不出现任何 SDK 类型。

## 业务侧用法

```go
type Service struct {
    sender mail.Sender
}

func (s *Service) SendWelcome(ctx context.Context, to mail.Address) error {
    _, err := s.sender.Send(ctx, mail.Message{
        From:    mail.Address{Name: "Gala", Email: "hello@example.com"},
        To:      []mail.Address{to},
        Subject: "欢迎使用 Gala",
        Text:    "欢迎加入。",
        HTML:    "<strong>欢迎加入。</strong>",
        Tags:    map[string]string{"kind": "welcome"},
    })
    return err
}
```

测试时注入 `mailtest.Recorder`，无需任何网络或 Mock 框架。

## 启动层组装

Resend（SDK Client 由应用创建并注入）：

```go
sdkClient := resendgo.NewClient(cfg.ResendAPIKey)
sender, err := mailresend.New(sdkClient.Emails)
```

SES v2（凭据、Region、Retryer、Tracing 全部由应用掌控）：

```go
awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.AWSRegion))
sesClient := sesv2.NewFromConfig(awsCfg)
sender, err := mailses.New(sesClient, mailses.WithConfigurationSet("production"))
```

SMTP（任意自建/企业邮件网关，底层 wneessen/go-mail；主机、认证、TLS 策略全部在官方 Client 上配置）：

```go
client, err := gomail.NewClient("smtp.example.com",
    gomail.WithTLSPolicy(gomail.TLSMandatory),
    gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
    gomail.WithUsername(cfg.SMTPUser), gomail.WithPassword(cfg.SMTPPass))
sender, err := mailsmtp.New(mailsmtp.NewDialer(client))
```

需要 Resend 幂等键的基础设施代码显式使用具体 Sender：

```go
receipt, err := sender.SendWithOptions(ctx, msg, mailresend.SendOptions{
    IdempotencyKey: "welcome/" + outboxMessage.ID,
})
```

## 必须理解的语义

- **accepted != delivered。** `Send` 成功仅表示 Provider 已接受请求；退信、投诉、最终投递状态由应用通过 Webhook、SNS 或 EventBridge 处理。
- **错误分类。** 校验失败满足 `errors.Is(err, mail.ErrInvalidMessage)`；调用 Client 前的 Context 错误原样返回；调用后的失败是 `*mail.DeliveryError`，`Unwrap` 保留原始 SDK 错误。
- **重试须同时检查两个维度。** 通用自动重试候选必须同时满足 `mail.IsTransient(err)` 与 `!mail.MayHaveBeenAccepted(err)`。`SendOutcomeUnknown` 表示请求可能已被接受，盲目重试会重复发信。
- **Resend 幂等键只保留 24 小时**，同一 Key 的所有重试必须使用完全相同的 payload。
- **消息预算。** 适配器默认在复制与序列化前按 10 MiB 近似原始输入预算拒绝超大消息（`WithMaxMessageBytes` 可调整）；提高预算不能绕过 Provider 硬限制。
- **库内不做队列、模板、额外重试与投递事件**；Outbox/Worker 属于应用层，见设计文档第 15 节。
- **SMTP 的语义差异。** SMTP 无云端事件回执，`Receipt.MessageID` 是适配器本地生成的 RFC 5322 Message-ID（`WithMessageIDGenerator` 可自定义）；协议阶段可见使其错误分类更精确——服务器不可达、MAIL FROM/RCPT TO 被拒等 DATA 终止序列前的失败都标记 `SendOutcomeNotAccepted`，满足瞬态条件即可进入通用安全重试路径（Resend/SES 大多数失败只能保守标记 Unknown）。每次 `Send` 建立并关闭一条连接，高吞吐场景应注入多个 Client/Sender。Tags 映射为 `X-Tag-<name>` 头，邮箱地址须为 ASCII（国际化域名先 punycode）。

## 测试

```bash
go test ./...                    # 默认不访问网络
go test -tags=integration ./...  # 需要 Provider 凭据与测试收件地址
```
