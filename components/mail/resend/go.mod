module github.com/apus-run/gala/components/mail/resend

go 1.25

require github.com/apus-run/gala/components/mail v0.10.0

require github.com/resend/resend-go/v3 v3.10.1

// TODO(release): 核心 v0.10.0 发布后删除本 replace（见设计文档 §12/§20，ADR-010）。
replace github.com/apus-run/gala/components/mail => ../
