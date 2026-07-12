package mail

// Attachment 表示一个邮件附件。首版使用 []byte 承载内容：
// 消息可安全重放，不会遇到 Reader 已消费或文件被删除等问题。
// 大附件不适合邮件通道；各 Provider 适配器会在复制与序列化前
// 执行有限的消息预算检查。
type Attachment struct {
	Filename    string
	ContentType string

	// ContentID 非空表示内嵌附件；HTML 中使用 cid:<ContentID> 引用。
	ContentID string
	Content   []byte
}

// Message 是 Provider 无关的邮件消息模型，只收纳所有 Provider
// 都能稳定表达的能力。Provider 特有能力（幂等键、定时发送、
// Configuration Set 等）由各具体 Sender 的扩展方法或构造选项承载。
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
