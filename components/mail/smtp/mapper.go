package smtp

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	gomail "github.com/wneessen/go-mail"

	"github.com/apus-run/gala/components/mail"
)

// buildMessage 把核心消息构造成 go-mail Msg，并返回写入 Receipt 的
// Message-ID（不含尖括号）。附件内容由 go-mail 读入自有缓冲，构造完成
// 后 Msg 不引用调用方数据。go-mail 的本地构造错误都发生在建连之前，
// 包装为 mail.ErrInvalidMessage。
func buildMessage(msg mail.Message, generateMessageID func() string) (*gomail.Msg, string, error) {
	gmsg := gomail.NewMsg()

	if err := gmsg.From(msg.From.String()); err != nil {
		return nil, "", fmt.Errorf("%w: smtp from address: %v", mail.ErrInvalidMessage, err)
	}
	for _, group := range []struct {
		field string
		add   func(string) error
		addrs []mail.Address
	}{
		{field: "to", add: gmsg.AddTo, addrs: msg.To},
		{field: "cc", add: gmsg.AddCc, addrs: msg.Cc},
		{field: "bcc", add: gmsg.AddBcc, addrs: msg.Bcc},
	} {
		for _, addr := range group.addrs {
			if err := group.add(addr.String()); err != nil {
				return nil, "", fmt.Errorf("%w: smtp %s address: %v", mail.ErrInvalidMessage, group.field, err)
			}
		}
	}
	if msg.ReplyTo != nil {
		if err := gmsg.ReplyTo(msg.ReplyTo.String()); err != nil {
			return nil, "", fmt.Errorf("%w: smtp reply-to address: %v", mail.ErrInvalidMessage, err)
		}
	}

	gmsg.Subject(msg.Subject)

	// Text 与 HTML 独立映射；两者都有时生成 multipart/alternative，
	// HTML 作为最后的 alternative 部分成为偏好呈现。
	switch {
	case msg.Text != "" && msg.HTML != "":
		gmsg.SetBodyString(gomail.TypeTextPlain, msg.Text)
		gmsg.AddAlternativeString(gomail.TypeTextHTML, msg.HTML)
	case msg.Text != "":
		gmsg.SetBodyString(gomail.TypeTextPlain, msg.Text)
	case msg.HTML != "":
		gmsg.SetBodyString(gomail.TypeTextHTML, msg.HTML)
	}

	// Headers 与 Tags 均按名排序写入，保证渲染结果与测试稳定。
	for _, name := range slices.Sorted(maps.Keys(msg.Headers)) {
		gmsg.SetGenHeader(gomail.Header(name), msg.Headers[name])
	}
	for _, name := range slices.Sorted(maps.Keys(msg.Tags)) {
		gmsg.SetGenHeader(gomail.Header(tagHeaderPrefix+name), msg.Tags[name])
	}

	for _, a := range msg.Attachments {
		if err := addAttachment(gmsg, a); err != nil {
			return nil, "", err
		}
	}

	gmsg.SetDate()

	messageID, err := applyMessageID(gmsg, generateMessageID)
	if err != nil {
		return nil, "", err
	}
	return gmsg, messageID, nil
}

func addAttachment(gmsg *gomail.Msg, a mail.Attachment) error {
	options := make([]gomail.FileOption, 0, 2)
	if a.ContentType != "" {
		options = append(options, gomail.WithFileContentType(gomail.ContentType(a.ContentType)))
	}
	if a.ContentID == "" {
		if err := gmsg.AttachReader(a.Filename, bytes.NewReader(a.Content), options...); err != nil {
			return fmt.Errorf("%w: smtp attachment %q: %v", mail.ErrInvalidMessage, a.Filename, err)
		}
		return nil
	}
	// go-mail 原样写入 Content-ID 头；RFC 2392 要求尖括号包裹，
	// 由适配器补齐（provider 校验已拒绝含尖括号的 ContentID）。
	options = append(options, gomail.WithFileContentID("<"+a.ContentID+">"))
	if err := gmsg.EmbedReader(a.Filename, bytes.NewReader(a.Content), options...); err != nil {
		return fmt.Errorf("%w: smtp inline attachment %q: %v", mail.ErrInvalidMessage, a.Filename, err)
	}
	return nil
}

// applyMessageID 写入 Message-ID 头并返回去尖括号后的值。
// 默认与自定义生成器的输出走同一校验（SPEC §3.2）。
func applyMessageID(gmsg *gomail.Msg, generate func() string) (string, error) {
	if generate == nil {
		gmsg.SetMessageID()
	} else {
		gmsg.SetMessageIDWithValue(generate())
	}
	id := strings.TrimSuffix(strings.TrimPrefix(gmsg.GetMessageID(), "<"), ">")
	if err := validateMessageID(id); err != nil {
		return "", err
	}
	return id, nil
}

// validateMessageSize 在任何缓冲与建连之前，检查近似原始消息总字节数
// （正文、Headers、Tags、地址与附件），字段口径与 resend/ses 适配器
// 一致。累加使用溢出安全加法；超限或溢出返回 mail.ErrInvalidMessage。
func validateMessageSize(msg mail.Message, maxBytes int64) error {
	size, ok := approximateMessageSize(msg)
	if !ok || size > maxBytes {
		return fmt.Errorf("%w: message exceeds %d byte budget", mail.ErrInvalidMessage, maxBytes)
	}
	return nil
}

func approximateMessageSize(msg mail.Message) (int64, bool) {
	var total int64
	ok := true
	add := func(n int) {
		v := int64(n)
		if total > math.MaxInt64-v {
			ok = false
			return
		}
		total += v
	}

	addAddress := func(a mail.Address) { add(len(a.Name)); add(len(a.Email)) }

	addAddress(msg.From)
	for _, group := range [][]mail.Address{msg.To, msg.Cc, msg.Bcc} {
		for _, addr := range group {
			addAddress(addr)
		}
	}
	if msg.ReplyTo != nil {
		addAddress(*msg.ReplyTo)
	}

	add(len(msg.Subject))
	add(len(msg.Text))
	add(len(msg.HTML))
	for name, value := range msg.Headers {
		add(len(name))
		add(len(value))
	}
	for name, value := range msg.Tags {
		add(len(name))
		add(len(value))
	}
	for _, a := range msg.Attachments {
		add(len(a.Filename))
		add(len(a.ContentType))
		add(len(a.ContentID))
		add(len(a.Content))
	}
	return total, ok
}
