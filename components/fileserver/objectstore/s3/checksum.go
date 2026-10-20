package s3

import (
	"crypto/sha256"
	"encoding/base64"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// checksumAlgorithm 是缓冲上传使用的附加校验和。
// 分片 ETag 在多片上传和 SSE-KMS 下不是内容 MD5，不能当作完整性证明。
// 请求里带上 SHA-256。响应若回显该值，必须与本地计算一致；没有回显不视为失败。
const checksumAlgorithm = types.ChecksumAlgorithmSha256

// partDone 是一片已确认的上传。sum 是标准 base64 的 SHA-256。
type partDone struct {
	number int32
	etag   string
	sum    string
}

func sha256Sum(data []byte) [32]byte { return sha256.Sum256(data) }

func encodeSHA256(sum [32]byte) string {
	return base64.StdEncoding.EncodeToString(sum[:])
}

// completedPart 把已确认分片还原成 CompleteMultipartUpload 需要的描述。
func completedPart(p partDone) types.CompletedPart {
	part := types.CompletedPart{
		ETag:           aws.String(p.etag),
		PartNumber:     aws.Int32(p.number),
		ChecksumSHA256: aws.String(p.sum),
	}
	return part
}
