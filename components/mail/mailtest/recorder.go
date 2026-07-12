// Package mailtest 提供 mail.Sender 的进程内假实现与共享契约测试，
// 供业务单元测试与 Provider 适配器测试复用。
package mailtest

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/apus-run/gala/components/mail"
)

// Recorder 是核心 Sender 的进程内假实现，记录收到的每条消息。
// 零值可用。
type Recorder struct {
	mu       sync.Mutex
	messages []mail.Message

	// Receipt 与 Err 必须在首次 Send 前配置，之后不得并发修改。
	Receipt mail.Receipt
	Err     error
}

var _ mail.Sender = (*Recorder)(nil)

// Send 记录消息的深拷贝，然后返回预配置的 Receipt 或 Err。
func (r *Recorder) Send(ctx context.Context, msg mail.Message) (mail.Receipt, error) {
	if err := ctx.Err(); err != nil {
		return mail.Receipt{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, cloneMessage(msg))
	if r.Err != nil {
		return mail.Receipt{}, r.Err
	}
	return r.Receipt, nil
}

// Messages 对每条消息再次深拷贝，可安全并发调用。
func (r *Recorder) Messages() []mail.Message {
	r.mu.Lock()
	defer r.mu.Unlock()

	messages := make([]mail.Message, len(r.messages))
	for i := range r.messages {
		messages[i] = cloneMessage(r.messages[i])
	}
	return messages
}

// cloneMessage 深拷贝 ReplyTo、所有地址 Slice、Headers、Tags、
// Attachments Slice 以及每个 Attachment.Content。
func cloneMessage(msg mail.Message) mail.Message {
	out := msg
	if msg.ReplyTo != nil {
		replyTo := *msg.ReplyTo
		out.ReplyTo = &replyTo
	}
	out.To = slices.Clone(msg.To)
	out.Cc = slices.Clone(msg.Cc)
	out.Bcc = slices.Clone(msg.Bcc)
	out.Headers = maps.Clone(msg.Headers)
	out.Tags = maps.Clone(msg.Tags)
	if msg.Attachments != nil {
		out.Attachments = make([]mail.Attachment, len(msg.Attachments))
		for i, a := range msg.Attachments {
			a.Content = bytes.Clone(a.Content)
			out.Attachments[i] = a
		}
	}
	return out
}
