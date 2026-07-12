package resend

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"maps"
	"math"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	resendgo "github.com/resend/resend-go/v3"

	"github.com/apus-run/gala/components/mail"
)

// mapMessage 把核心消息映射为 SDK 请求。所有 Map、Slice 与附件字节
// 均为深拷贝，映射完成后请求不再引用调用方数据。
func mapMessage(msg mail.Message) *resendgo.SendEmailRequest {
	request := &resendgo.SendEmailRequest{
		From:        msg.From.String(),
		To:          addressStrings(msg.To),
		Cc:          addressStrings(msg.Cc),
		Bcc:         addressStrings(msg.Bcc),
		Subject:     msg.Subject,
		Text:        msg.Text,
		Html:        msg.HTML,
		Headers:     maps.Clone(msg.Headers),
		Tags:        mapTags(msg.Tags),
		Attachments: mapAttachments(msg.Attachments),
	}
	if msg.ReplyTo != nil {
		request.ReplyTo = msg.ReplyTo.String()
	}
	return request
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

// mapTags 按 Key 排序，保证映射结果与测试稳定。
func mapTags(tags map[string]string) []resendgo.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]resendgo.Tag, 0, len(tags))
	for _, name := range slices.Sorted(maps.Keys(tags)) {
		out = append(out, resendgo.Tag{Name: name, Value: tags[name]})
	}
	return out
}

func mapAttachments(attachments []mail.Attachment) []*resendgo.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]*resendgo.Attachment, len(attachments))
	for i, a := range attachments {
		out[i] = &resendgo.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			ContentId:   a.ContentID,
			Content:     bytes.Clone(a.Content),
		}
	}
	return out
}

// Provider limits: https://resend.com/docs/api-reference/emails/send-email
const (
	maxResendRecipients      = 50
	maxResendTagLength       = 256
	maxResendContentIDLength = 128
	maxResendMessageBytes    = 40 << 20 // 40 MiB, after attachment Base64 encoding.
)

// unsupportedAttachmentExtensions mirrors Resend's current sending blocklist:
// https://resend.com/docs/knowledge-base/what-attachment-types-are-not-supported
var unsupportedAttachmentExtensions = map[string]struct{}{
	".adp": {}, ".app": {}, ".asp": {}, ".bas": {}, ".bat": {},
	".cer": {}, ".chm": {}, ".cmd": {}, ".com": {}, ".cpl": {},
	".crt": {}, ".csh": {}, ".der": {}, ".exe": {}, ".fxp": {},
	".gadget": {}, ".hlp": {}, ".hta": {}, ".inf": {}, ".ins": {},
	".isp": {}, ".its": {}, ".js": {}, ".jse": {}, ".ksh": {},
	".lib": {}, ".lnk": {}, ".mad": {}, ".maf": {}, ".mag": {},
	".mam": {}, ".maq": {}, ".mar": {}, ".mas": {}, ".mat": {},
	".mau": {}, ".mav": {}, ".maw": {}, ".mda": {}, ".mdb": {},
	".mde": {}, ".mdt": {}, ".mdw": {}, ".mdz": {}, ".msc": {},
	".msh": {}, ".msh1": {}, ".msh2": {}, ".mshxml": {}, ".msh1xml": {},
	".msh2xml": {}, ".msi": {}, ".msp": {}, ".mst": {}, ".ops": {},
	".pcd": {}, ".pif": {}, ".plg": {}, ".prf": {}, ".prg": {},
	".reg": {}, ".scf": {}, ".scr": {}, ".sct": {}, ".shb": {},
	".shs": {}, ".sys": {}, ".ps1": {}, ".ps1xml": {}, ".ps2": {},
	".ps2xml": {}, ".psc1": {}, ".psc2": {}, ".tmp": {}, ".url": {},
	".vb": {}, ".vbe": {}, ".vbs": {}, ".vps": {}, ".vsmacros": {},
	".vss": {}, ".vst": {}, ".vsw": {}, ".vxd": {}, ".ws": {},
	".wsc": {}, ".wsf": {}, ".wsh": {}, ".xnk": {},
}

// validateProviderMessage 检查 resend-go 不会在本地执行的 Provider 约束。
// 所有检查都发生在深拷贝和 Client 调用之前。
func validateProviderMessage(msg mail.Message) error {
	if len(msg.To) == 0 || len(msg.To) > maxResendRecipients {
		return fmt.Errorf("%w: resend to recipient count must be between 1 and %d",
			mail.ErrInvalidMessage, maxResendRecipients)
	}

	for name, value := range msg.Tags {
		if !validResendTagPart(name) {
			return fmt.Errorf("%w: invalid resend tag name", mail.ErrInvalidMessage)
		}
		if !validResendTagPart(value) {
			return fmt.Errorf("%w: invalid resend tag value", mail.ErrInvalidMessage)
		}
	}

	for _, attachment := range msg.Attachments {
		if attachment.ContentID != "" && utf8.RuneCountInString(attachment.ContentID) >= maxResendContentIDLength {
			return fmt.Errorf("%w: resend attachment content id must be shorter than %d characters",
				mail.ErrInvalidMessage, maxResendContentIDLength)
		}
		extension := strings.ToLower(path.Ext(strings.TrimSpace(attachment.Filename)))
		if _, unsupported := unsupportedAttachmentExtensions[extension]; unsupported {
			return fmt.Errorf("%w: resend does not support attachment extension %q",
				mail.ErrInvalidMessage, extension)
		}
	}

	size, ok := approximateResendEncodedMessageSize(msg)
	if !ok || size > maxResendMessageBytes {
		return fmt.Errorf("%w: message exceeds resend's %d byte encoded size limit",
			mail.ErrInvalidMessage, maxResendMessageBytes)
	}
	return nil
}

func validResendTagPart(value string) bool {
	if value == "" || len(value) > maxResendTagLength {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
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
	return approximateMessageSizeWithEncoding(msg, false)
}

func approximateResendEncodedMessageSize(msg mail.Message) (int64, bool) {
	return approximateMessageSizeWithEncoding(msg, true)
}

func approximateMessageSizeWithEncoding(msg mail.Message, encodeAttachments bool) (int64, bool) {
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
		contentSize := len(a.Content)
		if encodeAttachments {
			contentSize = base64.StdEncoding.EncodedLen(contentSize)
			if contentSize < 0 {
				ok = false
				continue
			}
		}
		add(contentSize)
	}
	return total, ok
}
