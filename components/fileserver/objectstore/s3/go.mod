module github.com/apus-run/gala/components/fileserver/objectstore/s3

go 1.27

require (
	github.com/apus-run/gala/components/fileserver v0.14.0
	github.com/aws/aws-sdk-go-v2 v1.47.3
	github.com/aws/aws-sdk-go-v2/service/s3 v1.114.3
	github.com/aws/smithy-go v1.28.5
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.22 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.6 // indirect
)

// TODO(release): 根模块 fileserver 发布后删除本 replace，并把 require 提到该版本。
// 发布顺序：先给 components/fileserver 打 tag，再改这里，最后给本模块打 tag（见 README.md 的“发布”）。
replace github.com/apus-run/gala/components/fileserver => ../../
