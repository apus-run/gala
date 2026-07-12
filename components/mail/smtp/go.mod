module github.com/apus-run/gala/components/mail/smtp

go 1.25.0

require (
	github.com/apus-run/gala/components/mail v0.10.0
	github.com/wneessen/go-mail v0.8.1
)

require (
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

// TODO(release): 核心 v0.10.0 发布后删除本 replace（见设计文档 §12/§20，ADR-010）。
replace github.com/apus-run/gala/components/mail => ../
