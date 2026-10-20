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

// 签名有效期的默认值与范围。上限是 SigV4 的 7 天。
const (
	DefaultExpiry = 15 * time.Minute
	MinExpiry     = time.Second
	MaxExpiry     = 7 * 24 * time.Hour
)

// ObjectStorage 是完整支持签名的对象存储。只有能同时实现这三组能力的类型才应声明它；
// 只需要上传的使用者可以只实现 fileserver.Uploader。
type ObjectStorage interface {
	fileserver.Storage
	// SignUploadReq 生成预签名上传请求。
	SignUploadReq(ctx context.Context, key string, opts ...SignOpt) (SignedRequest, error)
	// SignDownloadReq 生成预签名下载请求。
	SignDownloadReq(ctx context.Context, key string, opts ...SignOpt) (SignedRequest, error)
}

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

// SignOpt 是签名选项。方法私有，只能由本包构造。
type SignOpt interface {
	applySign(*signConfig)
}

type signConfig struct {
	expiry         time.Duration
	hasExpiry      bool
	contentType    string
	contentLength  int64
	hasLength      bool
	disposition    string
	hasDisposition bool
}

type signOptFunc func(*signConfig)

func (f signOptFunc) applySign(c *signConfig) { f(c) }

// WithExpiry 设置签名有效期，默认 DefaultExpiry，范围 [MinExpiry, MaxExpiry]。越界在 NewSignOptions 中返回 ErrInvalidArgument。
func WithExpiry(d time.Duration) SignOpt {
	return signOptFunc(func(c *signConfig) { c.expiry, c.hasExpiry = d, true })
}

// WithContentType 纳入签名头，客户端上传时必须发送相同的值。仅用于 SignUploadReq。
func WithContentType(s string) SignOpt {
	return signOptFunc(func(c *signConfig) { c.contentType = s })
}

// WithContentLength 纳入签名头，限制上传的确切大小。仅用于 SignUploadReq。
func WithContentLength(n int64) SignOpt {
	return signOptFunc(func(c *signConfig) { c.contentLength, c.hasLength = n, true })
}

// WithDisposition 设置下载时的 Content-Disposition 文件名。驱动负责编码。仅用于 SignDownloadReq。
func WithDisposition(filename string) SignOpt {
	return signOptFunc(func(c *signConfig) { c.disposition, c.hasDisposition = filename, true })
}

// SignOptions 是校验后的签名选项，只读。
type SignOptions struct {
	cfg signConfig
}

// NewSignOptions 应用并校验签名选项。nil 选项视为无操作。
// 方向相关的检查（例如在上传签名中使用 WithDisposition）由各驱动完成。
func NewSignOptions(opts ...SignOpt) (SignOptions, error) {
	var c signConfig
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
	return SignOptions{cfg: c}, nil
}

// Expiry 返回有效期；未设置时为 DefaultExpiry。
func (o SignOptions) Expiry() time.Duration {
	if !o.cfg.hasExpiry {
		return DefaultExpiry
	}
	return o.cfg.expiry
}

// ContentType 返回签名的内容类型；未设置时为空。
func (o SignOptions) ContentType() string { return o.cfg.contentType }

// ContentLength 返回签名的确切长度；未设置时第二个返回值为 false。
func (o SignOptions) ContentLength() (int64, bool) { return o.cfg.contentLength, o.cfg.hasLength }

// Disposition 返回下载文件名；未设置时第二个返回值为 false。
func (o SignOptions) Disposition() (string, bool) { return o.cfg.disposition, o.cfg.hasDisposition }
