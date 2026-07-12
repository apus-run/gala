package mail

// Receipt 表示 Provider 已接受发送请求。
//
// 接受不等于投递（accepted != delivered）：Provider 可能接受消息
// 但最终不发送。最终投递状态由应用通过 Provider 的事件机制获取。
type Receipt struct {
	// Provider 是接受请求的 Provider 标识，例如 "resend"、"ses"。
	Provider string
	// MessageID 是 Provider 返回的消息标识。
	MessageID string
}
