package ses

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"github.com/apus-run/gala/components/mail"
)

const charsetUTF8 = "UTF-8"

// mapMessage 用 SES Simple Content 构造请求。所有 Slice 与附件字节
// 均为深拷贝，映射完成后请求不再引用调用方数据。
func mapMessage(msg mail.Message, configurationSet string) *sesv2.SendEmailInput {
	input := &sesv2.SendEmailInput{
		FromEmailAddress: ptr(msg.From.String()),
		Destination: &types.Destination{
			ToAddresses:  addressStrings(msg.To),
			CcAddresses:  addressStrings(msg.Cc),
			BccAddresses: addressStrings(msg.Bcc),
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{
					Data:    ptr(msg.Subject),
					Charset: ptr(charsetUTF8),
				},
				Body:        mapBody(msg),
				Headers:     mapHeaders(msg.Headers),
				Attachments: mapAttachments(msg.Attachments),
			},
		},
		EmailTags: mapTags(msg.Tags),
	}
	if msg.ReplyTo != nil {
		input.ReplyToAddresses = []string{msg.ReplyTo.String()}
	}
	if configurationSet != "" {
		input.ConfigurationSetName = ptr(configurationSet)
	}
	return input
}

func mapBody(msg mail.Message) *types.Body {
	body := &types.Body{}
	if msg.Text != "" {
		body.Text = &types.Content{Data: ptr(msg.Text), Charset: ptr(charsetUTF8)}
	}
	if msg.HTML != "" {
		body.Html = &types.Content{Data: ptr(msg.HTML), Charset: ptr(charsetUTF8)}
	}
	return body
}

// mapHeaders 按 Name 排序，保证映射结果与测试稳定。
func mapHeaders(headers map[string]string) []types.MessageHeader {
	if len(headers) == 0 {
		return nil
	}
	out := make([]types.MessageHeader, 0, len(headers))
	for _, name := range slices.Sorted(maps.Keys(headers)) {
		out = append(out, types.MessageHeader{Name: ptr(name), Value: ptr(headers[name])})
	}
	return out
}

// mapTags 按 Name 排序，保证映射结果与测试稳定。
func mapTags(tags map[string]string) []types.MessageTag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]types.MessageTag, 0, len(tags))
	for _, name := range slices.Sorted(maps.Keys(tags)) {
		out = append(out, types.MessageTag{Name: ptr(name), Value: ptr(tags[name])})
	}
	return out
}

func mapAttachments(attachments []mail.Attachment) []types.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]types.Attachment, len(attachments))
	for i, a := range attachments {
		out[i] = mapAttachment(a)
	}
	return out
}

func mapAttachment(a mail.Attachment) types.Attachment {
	disposition := types.AttachmentContentDispositionAttachment
	if a.ContentID != "" {
		disposition = types.AttachmentContentDispositionInline
	}

	return types.Attachment{
		FileName:                ptr(a.Filename),
		RawContent:              bytes.Clone(a.Content),
		ContentType:             optionalString(a.ContentType),
		ContentId:               optionalString(a.ContentID),
		ContentDisposition:      disposition,
		ContentTransferEncoding: types.AttachmentContentTransferEncodingBase64,
	}
}

func addressStrings(addrs []mail.Address) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, len(addrs))
	for i, addr := range addrs {
		out[i] = addr.String()
	}
	return out
}

func ptr(s string) *string { return &s }

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// validateMessageSize 在任何深拷贝与 SDK 序列化之前，检查近似原始
// 消息总字节数（正文、Headers、Tags、地址与附件）。累加使用溢出安全
// 加法；超限或溢出返回 mail.ErrInvalidMessage，不调用 Client。
func validateMessageSize(msg mail.Message, maxBytes int64) error {
	size, ok := approximateMessageSize(msg)
	if !ok || size > maxBytes {
		return fmt.Errorf("%w: message exceeds %d byte budget", mail.ErrInvalidMessage, maxBytes)
	}
	return nil
}

func approximateMessageSize(msg mail.Message) (int64, bool) {
	return approximateMessageSizeWith(msg, func(content []byte) (int64, bool) {
		return int64(len(content)), true
	})
}

// approximateSESMessageSize 使用 SDK 传输附件时的 Base64 长度计算 SES
// 40 MiB 硬限制，其余字段仍按 UTF-8 字节数近似。
func approximateSESMessageSize(msg mail.Message) (int64, bool) {
	return approximateMessageSizeWith(msg, func(content []byte) (int64, bool) {
		encoded := base64.StdEncoding.EncodedLen(len(content))
		if encoded < 0 || encoded < len(content) {
			return 0, false
		}
		return int64(encoded), true
	})
}

func approximateMessageSizeWith(
	msg mail.Message,
	attachmentContentSize func([]byte) (int64, bool),
) (int64, bool) {
	var total int64
	ok := true
	add := func(n int64) {
		if n < 0 || total > math.MaxInt64-n {
			ok = false
			return
		}
		total += n
	}
	addString := func(value string) { add(int64(len(value))) }

	addAddress := func(a mail.Address) { addString(a.Name); addString(a.Email) }

	addAddress(msg.From)
	for _, group := range [][]mail.Address{msg.To, msg.Cc, msg.Bcc} {
		for _, addr := range group {
			addAddress(addr)
		}
	}
	if msg.ReplyTo != nil {
		addAddress(*msg.ReplyTo)
	}

	addString(msg.Subject)
	addString(msg.Text)
	addString(msg.HTML)
	for name, value := range msg.Headers {
		addString(name)
		addString(value)
	}
	for name, value := range msg.Tags {
		addString(name)
		addString(value)
	}
	for _, a := range msg.Attachments {
		addString(a.Filename)
		addString(a.ContentType)
		addString(a.ContentID)
		contentSize, contentOK := attachmentContentSize(a.Content)
		if !contentOK {
			ok = false
			continue
		}
		add(contentSize)
	}
	return total, ok
}
