package rid

import (
	"github.com/apus-run/gala/pkg/id"
)

const (
	// defaultCharset is the default alphabet | defaultCharset 定义默认的字符集.
	defaultCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

	// defaultIDLength is the length of the generated unique identifier | defaultIDLength 定义生成的唯一标识符长度.
	defaultIDLength = 6
)

// ResourceID represents a unique resource identifier | ResourceID 表示资源的唯一标识符.
type ResourceID string

// String implements the Stringer interface | String 实现 Stringer 接口.
func (rid ResourceID) String() string {
	return string(rid)
}

// NewResourceID creates a new resource identifier | NewResourceID 创建一个新的资源标识符.
func NewResourceID(prefix string) ResourceID {
	return ResourceID(prefix)
}

// New creates a prefixed unique identifier | New 创建带前缀的唯一标识符.
func (rid ResourceID) New(counter uint64) string {
	// generate the unique identifier with custom options
	// 使用自定义选项生成唯一标识符
	uniqueStr := id.NewCode(
		counter,
		id.WithCodeChars([]rune(defaultCharset)),
		id.WithCodeL(defaultIDLength),
		id.WithCodeSalt(Salt()),
	)
	return rid.String() + "-" + uniqueStr
}
