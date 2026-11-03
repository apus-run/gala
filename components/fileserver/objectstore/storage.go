// Package objectstore defines the optional signing capabilities of object
// storage. It depends only on the root package and the standard library;
// concrete cloud adapters (e.g. objectstore/s3) live in their own
// subdirectories.
// Package objectstore 定义对象存储的可选签名能力。它只依赖根包与标准库，
// 具体云适配器（如 objectstore/s3）在各自的子目录中实现。
package objectstore

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/apus-run/gala/components/fileserver"
)

// Default value and range of a signature's validity period. The upper bound
// is SigV4's 7 days.
// 签名有效期的默认值与范围。上限是 SigV4 的 7 天。
const (
	// Default signature expiry | 默认签名有效期。
	DefaultExpiry = 15 * time.Minute
	// Minimum signature expiry | 最小签名有效期。
	MinExpiry = time.Second
	// Maximum signature expiry | 最大签名有效期。
	MaxExpiry = 7 * 24 * time.Hour
)

// ObjectStorage is an object storage with full signing support. Only types
// that can implement all three groups of capabilities should declare it;
// upload-only users can implement just fileserver.Uploader.
// ObjectStorage 是完整支持签名的对象存储。只有能同时实现这三组能力的类型才应声明它；
// 只需要上传的使用者可以只实现 fileserver.Uploader。
type ObjectStorage interface {
	fileserver.Storage
	// SignUploadReq produces a presigned upload request.
	// SignUploadReq 生成预签名上传请求。
	SignUploadReq(ctx context.Context, key string, opts ...SignOpt) (SignedRequest, error)
	// SignDownloadReq produces a presigned download request.
	// SignDownloadReq 生成预签名下载请求。
	SignDownloadReq(ctx context.Context, key string, opts ...SignOpt) (SignedRequest, error)
}

// SignedRequest is a presigned request. When sending the request, the caller
// must carry the headers from Header verbatim.
//
// The URL is equivalent to a temporary credential: never write it into logs,
// error text or caches. Presigned direct upload bypasses the server, so the
// completion callback should verify the size with Stat; when size or type must
// be enforced, the matching options must be set at signing time.
// SignedRequest 是预签名请求。调用方发送请求时必须原样携带 Header 中的头。
//
// URL 等同于临时凭据：不得写入日志、错误文本或缓存。签名直传绕过了服务端，
// 因此完成回调中应使用 Stat 核对大小；需要强制大小或类型时，签名时必须设置对应选项。
type SignedRequest struct {
	Method    string
	URL       string
	Header    http.Header
	ExpiresAt time.Time
}

// SignOpt is a signing option. Its method is private: only this package can
// construct options.
// SignOpt 是签名选项。方法私有，只能由本包构造。
type SignOpt interface {
	applySign(*SignOptions)
}

type signOptFunc func(*SignOptions)

func (f signOptFunc) applySign(c *SignOptions) { f(c) }

// WithExpiry sets the signature validity period; defaults to DefaultExpiry,
// range [MinExpiry, MaxExpiry]. Out-of-range values make NewSignOptions return
// ErrInvalidArgument.
// WithExpiry 设置签名有效期，默认 DefaultExpiry，范围 [MinExpiry, MaxExpiry]。越界在 NewSignOptions 中返回 ErrInvalidArgument。
func WithExpiry(d time.Duration) SignOpt {
	return signOptFunc(func(c *SignOptions) { c.expiry, c.hasExpiry = d, true })
}

// WithContentType includes the content type in the signed headers; the client
// must send the same value when uploading. Only for SignUploadReq.
// WithContentType 纳入签名头，客户端上传时必须发送相同的值。仅用于 SignUploadReq。
func WithContentType(s string) SignOpt {
	return signOptFunc(func(c *SignOptions) { c.contentType = s })
}

// WithContentLength includes the exact upload size in the signed headers.
// Only for SignUploadReq.
// WithContentLength 纳入签名头，限制上传的确切大小。仅用于 SignUploadReq。
func WithContentLength(n int64) SignOpt {
	return signOptFunc(func(c *SignOptions) { c.contentLength, c.hasLength = n, true })
}

// WithDisposition sets the Content-Disposition filename for downloads. The
// driver takes care of encoding. Only for SignDownloadReq.
// WithDisposition 设置下载时的 Content-Disposition 文件名。驱动负责编码。仅用于 SignDownloadReq。
func WithDisposition(filename string) SignOpt {
	return signOptFunc(func(c *SignOptions) { c.disposition, c.hasDisposition = filename, true })
}

// SignOptions are the validated, read-only signing options.
// SignOptions 是校验后的签名选项，只读。
type SignOptions struct {
	expiry         time.Duration
	hasExpiry      bool
	contentType    string
	contentLength  int64
	hasLength      bool
	disposition    string
	hasDisposition bool
}

// NewSignOptions applies and validates the signing options. A nil option is a
// no-op. Direction-specific checks (e.g. using WithDisposition in an upload
// signature) are done by each driver.
// NewSignOptions 应用并校验签名选项。nil 选项视为无操作。
// 方向相关的检查（例如在上传签名中使用 WithDisposition）由各驱动完成。
func NewSignOptions(opts ...SignOpt) (SignOptions, error) {
	var c SignOptions
	for _, o := range opts {
		if o != nil {
			o.applySign(&c)
		}
	}
	if c.hasExpiry && (c.expiry < MinExpiry || c.expiry > MaxExpiry) {
		return SignOptions{}, fmt.Errorf("%w: expiry must be between %s and %s", fileserver.ErrInvalidArgument, MinExpiry, MaxExpiry)
	}
	if c.hasLength && c.contentLength < 0 {
		return SignOptions{}, fmt.Errorf("%w: content length must not be negative", fileserver.ErrInvalidArgument)
	}
	return c, nil
}

// Expiry returns the validity period; DefaultExpiry when unset.
// Expiry 返回有效期；未设置时为 DefaultExpiry。
func (o SignOptions) Expiry() time.Duration {
	if !o.hasExpiry {
		return DefaultExpiry
	}
	return o.expiry
}

// ContentType returns the signed content type; empty when unset.
// ContentType 返回签名的内容类型；未设置时为空。
func (o SignOptions) ContentType() string { return o.contentType }

// ContentLength returns the signed exact length; the second return value is
// false when unset.
// ContentLength 返回签名的确切长度；未设置时第二个返回值为 false。
func (o SignOptions) ContentLength() (int64, bool) { return o.contentLength, o.hasLength }

// Disposition returns the download filename; the second return value is false
// when unset.
// Disposition 返回下载文件名；未设置时第二个返回值为 false。
func (o SignOptions) Disposition() (string, bool) { return o.disposition, o.hasDisposition }
