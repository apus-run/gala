// Package smtp 基于 wneessen/go-mail 通过标准 SMTP 协议实现 mail.Sender。
//
// 适配器不创建网络栈：应用先用 gomail.NewClient 构造官方 Client
// （主机、端口、认证、TLS 策略、超时全部在此配置），包装为 Dialer 后注入：
//
//	client, err := gomail.NewClient("smtp.example.com",
//	    gomail.WithTLSPolicy(gomail.TLSMandatory),
//	    gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
//	    gomail.WithUsername(user), gomail.WithPassword(pass))
//	sender, err := smtp.New(smtp.NewDialer(client))
//
// 每次 Send 建立独立连接并在返回前关闭；Sender 无状态、无 Close，
// 构造后可并发使用。
//
// # 语义
//
// SMTP 服务器返回 250 仅表示消息已进入服务器队列，仍是
// accepted != delivered；SMTP 没有云端事件回执，Receipt.MessageID
// 是适配器本地生成的 RFC 5322 Message-ID（可用 WithMessageIDGenerator
// 自定义），供应用关联退信与日志。
//
// # 错误契约
//
// 遵循核心契约（见 mail 包文档）。SMTP 的协议阶段可见，分类比
// API Provider 更精确：拨号失败与 DATA 终止序列发出前被服务器拒绝
// （MAIL FROM、RCPT TO、DATA 命令等）都标记 SendOutcomeNotAccepted，
// 满足 IsTransient 时可进入通用安全重试路径；终止序列发出后的失败
// （响应错误或丢失）保持保守的 SendOutcomeUnknown。
// 发送成功后关闭连接失败不视为投递失败，仍返回成功 Receipt。
package smtp
