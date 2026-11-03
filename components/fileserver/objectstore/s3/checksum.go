package s3

import (
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// checksumAlgorithm is the additional checksum carried by buffered uploads.
// A part ETag is not a content MD5 under multipart upload or SSE-KMS and
// cannot serve as proof of integrity. The request carries SHA-256; if the
// response echoes the value it must match the local computation, and a
// missing echo is not treated as a failure.
// checksumAlgorithm 是缓冲上传使用的附加校验和。
// 分片 ETag 在多片上传和 SSE-KMS 下不是内容 MD5，不能当作完整性证明。
// 请求里带上 SHA-256。响应若回显该值，必须与本地计算一致；没有回显不视为失败。
const checksumAlgorithm = types.ChecksumAlgorithmSha256

func encodeSHA256(sum [32]byte) string {
	return base64.StdEncoding.EncodeToString(sum[:])
}
