package fileserver

import (
	"maps"
	"math"
	"strings"
)

// UploadOpt is an option for Upload. Its method is private: only this package
// can construct options.
// UploadOpt 是 Upload 的选项。方法私有，只能由本包构造。
type UploadOpt interface {
	applyUpload(*UploadOptions)
}

// DownloadOpt is an option for Download.
// DownloadOpt 是 Download 的选项。
type DownloadOpt interface {
	applyDownload(*DownloadOptions)
}

// ReadOpt is an option for Read.
// ReadOpt 是 Read 的选项。
type ReadOpt interface {
	applyRead(*ReadOptions)
}

type uploadOptFunc func(*UploadOptions)

func (f uploadOptFunc) applyUpload(c *UploadOptions) { f(c) }

// WithSize declares the exact length of the object. Strictly validated before
// commit: both short and overlong input returns ErrSizeMismatch. Negative
// values and MaxInt64 are rejected by NewUploadOptions with ErrInvalidArgument
// (the latter would overflow size+1).
// WithSize 声明对象的准确长度。提交前严格校验：短输入与超长输入都返回 ErrSizeMismatch。
// 负数与 MaxInt64 在 NewUploadOptions 中以 ErrInvalidArgument 拒绝（后者会使 size+1 溢出）。
func WithSize(n int64) UploadOpt {
	return uploadOptFunc(func(c *UploadOptions) { c.size, c.hasSize = n, true })
}

// WithContentType sets the persisted content type. Returns ErrUnsupported
// when the backend does not persist it.
// WithContentType 设置持久化的内容类型。后端不持久化时返回 ErrUnsupported。
func WithContentType(s string) UploadOpt {
	return uploadOptFunc(func(c *UploadOptions) { c.contentType = s })
}

// WithMetadata sets the persisted custom metadata. m is copied at call time;
// returns ErrUnsupported when the backend does not persist it.
// WithMetadata 设置持久化的自定义元数据。调用时复制 m；后端不持久化时返回 ErrUnsupported。
func WithMetadata(m map[string]string) UploadOpt {
	meta := maps.Clone(m)
	return uploadOptFunc(func(c *UploadOptions) { c.metadata = meta })
}

// WithIfNotExists requires the target to not exist. When the target already
// exists, returns ErrConflict and does not overwrite.
// WithIfNotExists 要求目标不存在。目标已存在时返回 ErrConflict 且不覆盖。
func WithIfNotExists() UploadOpt {
	return uploadOptFunc(func(c *UploadOptions) { c.ifNotExists = true })
}

// ProgressOpt injects progress observation; it applies to both Upload and
// Download.
// ProgressOpt 注入进度观察，同时适用于 Upload 与 Download。
type ProgressOpt struct {
	p Progress
}

func (o ProgressOpt) applyUpload(c *UploadOptions)     { c.progress = o.p }
func (o ProgressOpt) applyDownload(c *DownloadOptions) { c.progress = o.p }

// WithProgress injects a progress observer; disabled by default.
// WithProgress 注入进度观察者，默认关闭。
func WithProgress(p Progress) ProgressOpt {
	return ProgressOpt{p: p}
}

// RangeOpt specifies a byte range; it applies to both Download and Read. The
// zero value is a no-op, identical to passing no option.
// RangeOpt 指定字节范围，同时适用于 Download 与 Read。零值是无操作，与未传入选项相同。
type RangeOpt struct {
	set    bool
	off    int64
	length int64
}

func (r RangeOpt) validate() error {
	if !r.set {
		return nil
	}
	if r.off < 0 {
		return invalidArgument("range offset must not be negative")
	}
	if r.length < -1 {
		return invalidArgument("range length must be -1 or non-negative")
	}
	return nil
}

// bounds returns the range; (0, -1) when unset, i.e. the whole object.
// bounds 返回范围；未指定时为 (0, -1)，即整个对象。
func (r RangeOpt) bounds() (int64, int64) {
	if !r.set {
		return 0, -1
	}
	return r.off, r.length
}

func (o RangeOpt) applyDownload(c *DownloadOptions) {
	if o.set {
		c.rng = o
	}
}

func (o RangeOpt) applyRead(c *ReadOptions) {
	if o.set {
		c.rng = o
	}
}

// WithRange specifies a range starting at off with the given length. length
// -1 means "to the end"; a range past the end is clipped to the end. off and
// length are validated by New*Options.
// WithRange 指定从 off 开始、长度为 length 的范围。length 为 -1 表示到末尾；
// 超出末尾的部分截到末尾。off 与 length 的合法性在 New*Options 中校验。
func WithRange(off, length int64) RangeOpt {
	return RangeOpt{set: true, off: off, length: length}
}

// UploadOptions are the validated, read-only Upload options. The zero value
// means no options at all.
// UploadOptions 是校验后的 Upload 选项，只读。零值表示没有任何选项。
type UploadOptions struct {
	size        int64
	hasSize     bool
	contentType string
	metadata    map[string]string
	ifNotExists bool
	progress    Progress
}

// NewUploadOptions applies and validates Upload options. A nil option is a
// no-op.
// NewUploadOptions 应用并校验 Upload 选项。nil 选项视为无操作。
func NewUploadOptions(opts ...UploadOpt) (UploadOptions, error) {
	var c UploadOptions
	for _, o := range opts {
		if o != nil {
			o.applyUpload(&c)
		}
	}
	if c.hasSize && c.size < 0 {
		return UploadOptions{}, invalidArgument("size must not be negative")
	}
	if c.hasSize && c.size == math.MaxInt64 {
		return UploadOptions{}, invalidArgument("size must be less than MaxInt64")
	}
	if hasControl(c.contentType) {
		return UploadOptions{}, invalidArgument("content type contains control character")
	}
	meta, err := normalizeMetadata(c.metadata)
	if err != nil {
		return UploadOptions{}, err
	}
	c.metadata = meta
	return c, nil
}

// Size returns the declared length; the second return value is false when
// undeclared. A declared 0 is a valid empty object, distinct from undeclared.
// Size 返回声明的长度；未声明时第二个返回值为 false。
// 声明的 0 是合法的空文件，与未声明不同。
func (o UploadOptions) Size() (int64, bool) { return o.size, o.hasSize }

// ContentType returns the content type; empty when unset.
// ContentType 返回内容类型；未设置时为空。
func (o UploadOptions) ContentType() string { return o.contentType }

// Metadata returns a copy of the metadata with lowercased keys; nil when there
// is none.
// Metadata 返回元数据的副本，键已小写；没有时返回 nil。
func (o UploadOptions) Metadata() map[string]string { return maps.Clone(o.metadata) }

// IfNotExists reports whether the target is required to not exist.
// IfNotExists 报告是否要求目标不存在。
func (o UploadOptions) IfNotExists() bool { return o.ifNotExists }

// Progress returns the progress observer; nil when unset, callers must
// nil-check.
// Progress 返回进度观察者；未设置时返回 nil，调用方需判空。
func (o UploadOptions) Progress() Progress { return o.progress }

// DownloadOptions are the validated, read-only Download options. The zero
// value means the whole object with no progress observer.
// DownloadOptions 是校验后的 Download 选项，只读。零值表示整个对象、无进度观察。
type DownloadOptions struct {
	rng      RangeOpt
	progress Progress
}

// NewDownloadOptions applies and validates Download options. A nil option is a
// no-op.
// NewDownloadOptions 应用并校验 Download 选项。nil 选项视为无操作。
func NewDownloadOptions(opts ...DownloadOpt) (DownloadOptions, error) {
	var c DownloadOptions
	for _, o := range opts {
		if o != nil {
			o.applyDownload(&c)
		}
	}
	if err := c.rng.validate(); err != nil {
		return DownloadOptions{}, err
	}
	return c, nil
}

// Range returns the range (off, length); (0, -1) when unset. length -1 means
// "to the end".
// Range 返回范围 (off, length)；未指定时为 (0, -1)。length 为 -1 表示到末尾。
func (o DownloadOptions) Range() (int64, int64) { return o.rng.bounds() }

// Progress returns the progress observer; nil when unset.
// Progress 返回进度观察者；未设置时返回 nil。
func (o DownloadOptions) Progress() Progress { return o.progress }

// ReadOptions are the validated, read-only Read options. The zero value means
// the whole object.
// ReadOptions 是校验后的 Read 选项，只读。零值表示整个对象。
type ReadOptions struct {
	rng RangeOpt
}

// NewReadOptions applies and validates Read options. A nil option is a no-op.
// NewReadOptions 应用并校验 Read 选项。nil 选项视为无操作。
func NewReadOptions(opts ...ReadOpt) (ReadOptions, error) {
	var c ReadOptions
	for _, o := range opts {
		if o != nil {
			o.applyRead(&c)
		}
	}
	if err := c.rng.validate(); err != nil {
		return ReadOptions{}, err
	}
	return c, nil
}

// Range returns the range (off, length); (0, -1) when unset.
// Range 返回范围 (off, length)；未指定时为 (0, -1)。
func (o ReadOptions) Range() (int64, int64) { return o.rng.bounds() }

// normalizeMetadata returns a new map with lowercased keys; nil when there is
// no metadata. Two keys that differ in case but collide after lowercasing are
// treated as a conflict.
// normalizeMetadata 返回键已小写的新 map；没有元数据时返回 nil。
// 大小写不同而小写后相同的两个键视为冲突。
func normalizeMetadata(m map[string]string) (map[string]string, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		if lk == "" {
			return nil, invalidArgument("metadata key must not be empty")
		}
		if _, dup := out[lk]; dup {
			return nil, invalidArgument("metadata keys collide after lowercasing")
		}
		out[lk] = v
	}
	return out, nil
}
