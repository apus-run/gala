package mail

// Attachment 表示一个邮件附件。首版使用 []byte 承载内容：
// 消息可安全重放，不会遇到 Reader 已消费或文件被删除等问题。
// 大附件不适合邮件通道；各 Provider 适配器会在复制与序列化前
// 执行有限的消息预算检查。
// Attachment is an email attachment. This first version carries content as
// []byte so a message can be replayed safely without hitting consumed readers
// or deleted files. Large attachments do not fit an email channel; provider
// adapters run bounded message budget checks before copying and serializing.
type Attachment struct {
	// Filename is the attachment file name | 附件文件名.
	Filename string
	// ContentType 是 MIME 类型 | MIME 类型.
	ContentType string

	// ContentID 非空表示内嵌附件；HTML 中使用 cid:<ContentID> 引用。
	// A non-empty ContentID marks an inline attachment referenced as cid:<ContentID> in HTML.
	ContentID string
	// Content is the attachment bytes | 附件字节内容.
	Content []byte
}

// Message 是 Provider 无关的邮件消息模型，只收纳所有 Provider
// 都能稳定表达的能力。Provider 特有能力（幂等键、定时发送、
// Configuration Set 等）由各具体 Sender 的扩展方法或构造选项承载。
// Message is the provider-agnostic email model, limited to what every provider
// expresses stably. Provider-specific capabilities (idempotency keys,
// scheduled sending, configuration sets, ...) live in each Sender's extension
// methods or constructor options.
type Message struct {
	From    Address
	To      []Address
	Cc      []Address
	Bcc     []Address
	ReplyTo *Address

	Subject string
	Text    string
	HTML    string

	Headers map[string]string
	Tags    map[string]string

	Attachments []Attachment
}
