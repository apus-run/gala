package mail

// Receipt 表示 Provider 已接受发送请求。
//
// 接受不等于投递（accepted != delivered）：Provider 可能接受消息
// 但最终不发送。最终投递状态由应用通过 Provider 的事件机制获取。
// Receipt means the provider accepted the send request.
//
// Accepted is not delivered: a provider may accept a message yet never send
// it. Final delivery status is obtained via the provider's event mechanism.
type Receipt struct {
	// Provider 是接受请求的 Provider 标识，例如 "resend"、"ses"。
	// Provider is the identifier of the accepting provider, e.g. "resend", "ses".
	Provider string
	// MessageID 是 Provider 返回的消息标识。
	// MessageID is the message identifier returned by the provider.
	MessageID string
}
