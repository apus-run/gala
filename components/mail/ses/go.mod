module github.com/apus-run/gala/components/mail/ses

go 1.25

require (
	github.com/apus-run/gala/components/mail v0.10.0
	github.com/aws/aws-sdk-go-v2 v1.42.1
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.63.0
	github.com/aws/smithy-go v1.27.3
)

require (
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.31 // indirect
)

// TODO(release): 核心 v0.10.0 发布后删除本 replace（见设计文档 §12/§20，ADR-010）。
replace github.com/apus-run/gala/components/mail => ../
