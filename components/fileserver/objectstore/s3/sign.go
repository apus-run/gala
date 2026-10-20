package s3

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/objectstore"
)

// SignUploadReq 实现 objectstore.ObjectStorage。签名在本地计算，不访问存储服务。
// 返回的 Header 是签名的一部分，调用方发送请求时必须原样携带。
func (s *Store) SignUploadReq(ctx context.Context, key string, opts ...objectstore.SignOpt) (objectstore.SignedRequest, error) {
	const op = "SignUploadReq"
	so, objKey, err := s.prepareSign(ctx, op, key, opts)
	if err != nil {
		return objectstore.SignedRequest{}, err
	}
	if _, ok := so.Disposition(); ok {
		return objectstore.SignedRequest{}, opErr(op, key, invalidArgument("disposition is only valid for download signatures"))
	}
	if err := validateContentType(so.ContentType()); err != nil {
		return objectstore.SignedRequest{}, opErr(op, key, err)
	}

	in := &awss3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objKey)}
	if ct := so.ContentType(); ct != "" {
		in.ContentType = aws.String(ct)
	}
	if n, ok := so.ContentLength(); ok {
		in.ContentLength = aws.Int64(n)
	}
	req, err := s.presign.PresignPutObject(ctx, in, expires(so.Expiry()))
	if err != nil {
		return objectstore.SignedRequest{}, opErr(op, key, hidePresignErr(err))
	}
	return signedRequest(req, http.MethodPut, so.Expiry()), nil
}

// SignDownloadReq 实现 objectstore.ObjectStorage。签名在本地计算，不访问存储服务。
func (s *Store) SignDownloadReq(ctx context.Context, key string, opts ...objectstore.SignOpt) (objectstore.SignedRequest, error) {
	const op = "SignDownloadReq"
	so, objKey, err := s.prepareSign(ctx, op, key, opts)
	if err != nil {
		return objectstore.SignedRequest{}, err
	}
	if so.ContentType() != "" || hasSignLength(so) {
		return objectstore.SignedRequest{}, opErr(op, key, invalidArgument("content type and content length are only valid for upload signatures"))
	}

	in := &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objKey)}
	if name, ok := so.Disposition(); ok {
		disp, err := contentDisposition(name)
		if err != nil {
			return objectstore.SignedRequest{}, opErr(op, key, err)
		}
		in.ResponseContentDisposition = aws.String(disp)
	}
	req, err := s.presign.PresignGetObject(ctx, in, expires(so.Expiry()))
	if err != nil {
		return objectstore.SignedRequest{}, opErr(op, key, hidePresignErr(err))
	}
	return signedRequest(req, http.MethodGet, so.Expiry()), nil
}

// prepareSign 校验 Key、选项与签名器。未配置签名器时返回 ErrUnsupported，且不产生签名。
func (s *Store) prepareSign(ctx context.Context, op, key string, opts []objectstore.SignOpt) (objectstore.SignOptions, string, error) {
	objKey, err := s.objectKey(key)
	if err != nil {
		return objectstore.SignOptions{}, "", opErr(op, key, err)
	}
	so, err := objectstore.NewSignOptions(opts...)
	if err != nil {
		return objectstore.SignOptions{}, "", opErr(op, key, err)
	}
	if s.presign == nil {
		return objectstore.SignOptions{}, "", opErr(op, key, fmt.Errorf("%w: presigner is not configured", fileserver.ErrUnsupported))
	}
	if err := ctx.Err(); err != nil {
		return objectstore.SignOptions{}, "", opErr(op, key, err)
	}
	return so, objKey, nil
}

func hasSignLength(so objectstore.SignOptions) bool {
	_, ok := so.ContentLength()
	return ok
}

func expires(d time.Duration) func(*awss3.PresignOptions) {
	return func(o *awss3.PresignOptions) { o.Expires = d }
}

// signedRequest 组装预签名结果。Header 是副本，调用方修改它不影响驱动。
// 本函数不把 URL 写入错误或日志。
func signedRequest(req *v4.PresignedHTTPRequest, method string, expiry time.Duration) objectstore.SignedRequest {
	header := req.SignedHeader.Clone()
	if header == nil {
		header = make(http.Header)
	}
	gotMethod := req.Method
	if gotMethod == "" {
		gotMethod = method
	}
	return objectstore.SignedRequest{
		Method:    gotMethod,
		URL:       req.URL,
		Header:    header,
		ExpiresAt: time.Now().Add(expiry),
	}
}

// opaquePresignError 保留原始原因，但错误文本不回显签名 URL。
type opaquePresignError struct{ err error }

func (e opaquePresignError) Error() string { return "presign failed" }
func (e opaquePresignError) Unwrap() error { return e.err }

func hidePresignErr(err error) error {
	return opaquePresignError{err: err}
}

// contentDisposition 生成下载签名使用的 Content-Disposition。
// filename 是 ASCII 回退名，filename* 按 RFC 5987 百分号编码原始文件名。
func contentDisposition(name string) (string, error) {
	if name == "" {
		return "", invalidArgument("disposition filename is empty")
	}
	for i := range len(name) {
		if name[i] < 0x20 || name[i] == 0x7f {
			return "", invalidArgument("disposition filename contains a control character")
		}
	}
	var quoted strings.Builder
	quoted.WriteString(`attachment; filename="`)
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			quoted.WriteByte('_')
			continue
		}
		quoted.WriteRune(r)
	}
	quoted.WriteString(`"; filename*=UTF-8''`)
	quoted.WriteString(encodeRFC5987(name))
	return quoted.String(), nil
}

// encodeRFC5987 按 RFC 5987 的 attr-char 白名单做百分号编码。
// url.PathEscape 是路径段编码，会原样保留 ':'、'@'、'=' 等非 attr-char。
func encodeRFC5987(s string) string {
	const attrChar = "!#$&+-.^_`|~"
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case strings.IndexByte(attrChar, c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
