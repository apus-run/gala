package mail

import (
	netmail "net/mail" // 标准库包名与本包 mail 冲突，需别名
)

// Address 表示一个邮件地址及其可选显示名。
type Address struct {
	Name  string
	Email string
}

// String 返回 RFC 5322 地址文本，例如 "Gala <hello@example.com>"。
// 非 ASCII 显示名由标准库按 RFC 2047 编码。
func (a Address) String() string {
	return (&netmail.Address{Name: a.Name, Address: a.Email}).String()
}
