package ses

import (
	"fmt"
	"net/textproto"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/apus-run/gala/components/mail"
)

const (
	maxRecipients          = 50
	maxHeaders             = 15
	maxHeaderNameLength    = 126
	maxHeaderValueLength   = 995
	maxHeaderCombined      = 996
	maxTagLength           = 256
	maxAttachmentName      = 255
	maxAttachmentContentID = 78
	maxAttachmentType      = 78
	maxConfigurationSet    = 64
	maxSESMessageBytes     = 40 << 20 // 40 MiB after attachment base64 encoding.
)

// sesReservedHeaders 补充核心公共模型之外，SES Simple Content 禁止设置的
// 自定义 Header。其他结构 Header 已由 mail.Message.Validate 拒绝。
var sesReservedHeaders = map[string]struct{}{
	"Content-Disposition": {},
	"Date":                {},
	"Message-Id":          {},
	"Return-Path":         {},
}

// unsupportedAttachmentExtensions 来自 Amazon SES Developer Guide 的
// “SES unsupported attachment types”清单。比较时不区分大小写。
var unsupportedAttachmentExtensions = map[string]struct{}{
	".ade": {}, ".adp": {}, ".app": {}, ".asp": {}, ".bas": {}, ".bat": {},
	".cer": {}, ".chm": {}, ".cmd": {}, ".com": {}, ".cpl": {}, ".crt": {},
	".csh": {}, ".der": {}, ".exe": {}, ".fxp": {}, ".gadget": {}, ".hlp": {},
	".hta": {}, ".inf": {}, ".ins": {}, ".isp": {}, ".its": {}, ".js": {},
	".jse": {}, ".ksh": {}, ".lib": {}, ".lnk": {}, ".mad": {}, ".maf": {},
	".mag": {}, ".mam": {}, ".maq": {}, ".mar": {}, ".mas": {}, ".mat": {},
	".mau": {}, ".mav": {}, ".maw": {}, ".mda": {}, ".mdb": {}, ".mde": {},
	".mdt": {}, ".mdw": {}, ".mdz": {}, ".msc": {}, ".msh": {}, ".msh1": {},
	".msh2": {}, ".mshxml": {}, ".msh1xml": {}, ".msh2xml": {}, ".msi": {},
	".msp": {}, ".mst": {}, ".ops": {}, ".pcd": {}, ".pif": {}, ".plg": {},
	".prf": {}, ".prg": {}, ".reg": {}, ".scf": {}, ".scr": {}, ".sct": {},
	".shb": {}, ".shs": {}, ".sys": {}, ".ps1": {}, ".ps1xml": {}, ".ps2": {},
	".ps2xml": {}, ".psc1": {}, ".psc2": {}, ".tmp": {}, ".url": {}, ".vb": {},
	".vbe": {}, ".vbs": {}, ".vps": {}, ".vsmacros": {}, ".vss": {}, ".vst": {},
	".vsw": {}, ".vxd": {}, ".ws": {}, ".wsc": {}, ".wsf": {}, ".wsh": {},
	".xnk": {},
}

// validateProviderMessage 检查 SES API v2 的稳定输入约束。调用方必须先
// 执行 mail.Message.Validate；这里的错误同样属于未调用 Provider 的
// mail.ErrInvalidMessage。
func validateProviderMessage(msg mail.Message) error {
	if err := validateRecipientCount(msg); err != nil {
		return err
	}
	if err := validateSESAddresses(msg); err != nil {
		return err
	}
	if err := validateSESHeaders(msg.Headers); err != nil {
		return err
	}
	if err := validateSESTags(msg.Tags); err != nil {
		return err
	}
	if err := validateSESAttachments(msg.Attachments); err != nil {
		return err
	}
	if err := validateSESHardMessageSize(msg); err != nil {
		return err
	}
	return nil
}

func validateRecipientCount(msg mail.Message) error {
	count := 0
	for _, addresses := range [][]mail.Address{msg.To, msg.Cc, msg.Bcc} {
		if len(addresses) > maxRecipients-count {
			return fmt.Errorf("%w: ses supports at most %d recipients", mail.ErrInvalidMessage, maxRecipients)
		}
		count += len(addresses)
	}
	return nil
}

func validateSESAddresses(msg mail.Message) error {
	if err := validateSESAddress("from", msg.From); err != nil {
		return err
	}
	for _, group := range []struct {
		field     string
		addresses []mail.Address
	}{
		{field: "to", addresses: msg.To},
		{field: "cc", addresses: msg.Cc},
		{field: "bcc", addresses: msg.Bcc},
	} {
		for _, address := range group.addresses {
			if err := validateSESAddress(group.field, address); err != nil {
				return err
			}
		}
	}
	if msg.ReplyTo != nil {
		return validateSESAddress("reply-to", *msg.ReplyTo)
	}
	return nil
}

func validateSESAddress(field string, address mail.Address) error {
	if !isASCII(address.Email) {
		return fmt.Errorf("%w: ses %s email must be 7-bit ASCII; encode international domains with punycode",
			mail.ErrInvalidMessage, field)
	}
	return nil
}

func validateSESHeaders(headers map[string]string) error {
	if len(headers) > maxHeaders {
		return fmt.Errorf("%w: ses supports at most %d custom headers", mail.ErrInvalidMessage, maxHeaders)
	}
	for name, value := range headers {
		if len(name) == 0 || len(name) > maxHeaderNameLength || !isSESHeaderName(name) {
			return fmt.Errorf("%w: ses header %q has an invalid name", mail.ErrInvalidMessage, name)
		}
		if len(value) == 0 || len(value) > maxHeaderValueLength || !isSESHeaderValue(value) {
			return fmt.Errorf("%w: ses header %q has an invalid value", mail.ErrInvalidMessage, name)
		}
		if len(name)+len(value) > maxHeaderCombined {
			return fmt.Errorf("%w: ses header %q exceeds the %d character combined limit",
				mail.ErrInvalidMessage, name, maxHeaderCombined)
		}
		if _, reserved := sesReservedHeaders[textproto.CanonicalMIMEHeaderKey(name)]; reserved {
			return fmt.Errorf("%w: ses header %q is reserved", mail.ErrInvalidMessage, name)
		}
	}
	return nil
}

func validateSESTags(tags map[string]string) error {
	for name, value := range tags {
		if len(name) == 0 || len(name) > maxTagLength || !isSESTagText(name) {
			return fmt.Errorf("%w: ses tag has an invalid name", mail.ErrInvalidMessage)
		}
		if len(value) == 0 || len(value) > maxTagLength || !isSESTagText(value) {
			return fmt.Errorf("%w: ses tag %q has an invalid value", mail.ErrInvalidMessage, name)
		}
	}
	return nil
}

func validateSESAttachments(attachments []mail.Attachment) error {
	for i, attachment := range attachments {
		if utf8.RuneCountInString(attachment.Filename) > maxAttachmentName {
			return fmt.Errorf("%w: ses attachment %d filename exceeds %d characters",
				mail.ErrInvalidMessage, i, maxAttachmentName)
		}
		if attachment.ContentID != "" && utf8.RuneCountInString(attachment.ContentID) > maxAttachmentContentID {
			return fmt.Errorf("%w: ses attachment %q content id exceeds %d characters",
				mail.ErrInvalidMessage, attachment.Filename, maxAttachmentContentID)
		}
		if attachment.ContentType != "" && utf8.RuneCountInString(attachment.ContentType) > maxAttachmentType {
			return fmt.Errorf("%w: ses attachment %q content type exceeds %d characters",
				mail.ErrInvalidMessage, attachment.Filename, maxAttachmentType)
		}
		extension := strings.ToLower(path.Ext(strings.TrimSpace(attachment.Filename)))
		if _, unsupported := unsupportedAttachmentExtensions[extension]; unsupported {
			return fmt.Errorf("%w: ses does not support attachment extension %q",
				mail.ErrInvalidMessage, extension)
		}
	}
	return nil
}

func validateSESHardMessageSize(msg mail.Message) error {
	size, ok := approximateSESMessageSize(msg)
	if !ok || size > maxSESMessageBytes {
		return fmt.Errorf("%w: ses message exceeds %d byte hard limit after attachment base64 encoding",
			mail.ErrInvalidMessage, maxSESMessageBytes)
	}
	return nil
}

func validateConfigurationSetName(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > maxConfigurationSet || !isSESTagText(name) {
		return fmt.Errorf("mail/ses: configuration set name must be at most %d ASCII letters, digits, underscores or dashes",
			maxConfigurationSet)
	}
	return nil
}

func isASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] > 0x7f {
			return false
		}
	}
	return true
}

func isSESHeaderName(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 33 || value[i] > 126 || value[i] == ':' {
			return false
		}
	}
	return true
}

func isSESHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 32 || value[i] > 126 {
			return false
		}
	}
	return true
}

func isSESTagText(value string) bool {
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
