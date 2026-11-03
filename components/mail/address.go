package mail

import (
	netmail "net/mail" // 标准库包名与本包 mail 冲突，需别名 | the stdlib package name clashes with this mail package, hence the alias
)

// Address 表示一个邮件地址及其可选显示名。
// Address is an email address with an optional display name.
type Address struct {
	// Name is the display name | 显示名.
	Name string
	// Email is the mailbox address | 邮箱地址.
	Email string
}

// String 返回 RFC 5322 地址文本，例如 "Gala <hello@example.com>"。
// 非 ASCII 显示名由标准库按 RFC 2047 编码。
// String returns RFC 5322 address text, e.g. "Gala <hello@example.com>";
// non-ASCII display names are encoded per RFC 2047 by the stdlib.
func (a Address) String() string {
	return (&netmail.Address{Name: a.Name, Address: a.Email}).String()
}
