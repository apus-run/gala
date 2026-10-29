// Package sonic provides the optional Sonic JSON codec without a global registry.
package sonic

import (
	"errors"

	"github.com/bytedance/sonic"
)

// ErrTooLarge 表示载荷超过 Codec.MaxBytes。
var ErrTooLarge = errors.New("sonic: payload exceeds size limit")

// defaultMaxBytes 与 msgpack codec 的默认解码上限一致。
const defaultMaxBytes = 16 << 20

// Codec 是 Sonic JSON 编解码器。
//
// 缓存里的数据可能被写入方（或被攻破的 Redis）替换成超大载荷，解码前先限制大小，
// 否则一个条目就能占用大量内存。MaxBytes 为 0 时使用 16 MiB，必须非负。
type Codec struct{ MaxBytes int }

func (c Codec) limit() (int, error) {
	if c.MaxBytes < 0 {
		return 0, errors.New("sonic: invalid size limit")
	}
	if c.MaxBytes == 0 {
		return defaultMaxBytes, nil
	}
	return c.MaxBytes, nil
}

func (c Codec) Marshal(v any) ([]byte, error) {
	limit, err := c.limit()
	if err != nil {
		return nil, err
	}
	b, err := sonic.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, ErrTooLarge
	}
	return b, nil
}

func (c Codec) Unmarshal(b []byte, v any) error {
	limit, err := c.limit()
	if err != nil {
		return err
	}
	if len(b) > limit {
		return ErrTooLarge
	}
	return sonic.Unmarshal(b, v)
}
