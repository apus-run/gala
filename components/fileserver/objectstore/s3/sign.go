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

// SignUploadReq implements objectstore.ObjectStorage. The signature is
// computed locally without contacting the storage service. The returned
// Header is part of the signature and the caller must send it verbatim
// with the request.
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

	// When MaxObjectSize is configured the signature must carry a length
	// within the limit: S3 only enforces the signed Content-Length, and a
	// presigned URL without a length can upload an object of any size,
	// bypassing the same cap on the direct upload path (upload.go).
	// 配置了 MaxObjectSize 时，签名里必须带上长度并且不超限：S3 只会强制执行「已签名的」Content-Length，
	// 不带长度的预签名 URL 可以上传任意大小的对象，绕过直接上传路径（upload.go）上同样的上限。
	n, hasLen := so.ContentLength()
	if s.maxObject > 0 {
		if !hasLen {
			return objectstore.SignedRequest{}, opErr(op, key, invalidArgument("content length is required when MaxObjectSize is configured"))
		}
		if n > s.maxObject {
			return objectstore.SignedRequest{}, opErr(op, key, tooLarge())
		}
	}

	in := &awss3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objKey)}
	if ct := so.ContentType(); ct != "" {
		in.ContentType = aws.String(ct)
	}
	if hasLen {
		in.ContentLength = aws.Int64(n)
	}
	req, err := s.presign.PresignPutObject(ctx, in, expires(so.Expiry()))
	if err != nil {
		return objectstore.SignedRequest{}, opErr(op, key, hidePresignErr(err))
	}
	return signedRequest(req, http.MethodPut, so.Expiry()), nil
}

// SignDownloadReq implements objectstore.ObjectStorage. The signature is
// computed locally without contacting the storage service.
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

// prepareSign validates the key, options and presigner. It returns
// ErrUnsupported without producing a signature when no presigner is
// configured.
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

// signedRequest assembles the presigned result. Header is a copy, so
// caller mutations do not affect the driver. This function never writes
// the URL into errors or logs.
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

// opaquePresignError keeps the original cause, but its error text never
// echoes the presigned URL.
// opaquePresignError 保留原始原因，但错误文本不回显签名 URL。
type opaquePresignError struct{ err error }

// Error returns a message that never echoes the URL | Error 返回不回显 URL 的消息.
func (e opaquePresignError) Error() string { return "presign failed" }

// Unwrap returns the original presign error | Unwrap 返回原始的预签名错误.
func (e opaquePresignError) Unwrap() error { return e.err }

func hidePresignErr(err error) error {
	return opaquePresignError{err: err}
}

// contentDisposition builds the Content-Disposition used by download
// signatures. filename is the ASCII fallback and filename* percent-encodes
// the original file name per RFC 5987.
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

// encodeRFC5987 percent-encodes against RFC 5987's attr-char whitelist.
// url.PathEscape is path-segment encoding and leaves ':', '@', '=' and
// other non attr-chars untouched.
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
