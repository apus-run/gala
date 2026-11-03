// Package sonic provides the optional Sonic JSON codec without a global registry.
// sonic 提供可选的 Sonic JSON 编解码器，不使用全局注册表。
package sonic

import (
	"errors"

	"github.com/bytedance/sonic"
)

// ErrTooLarge means the payload exceeds Codec.MaxBytes.
// ErrTooLarge 表示载荷超过 Codec.MaxBytes。
var ErrTooLarge = errors.New("sonic: payload exceeds size limit")

// defaultMaxBytes matches the msgpack codec's default decode limit.
// defaultMaxBytes 与 msgpack codec 的默认解码上限一致。
const defaultMaxBytes = 16 << 20

// Codec is the Sonic JSON codec | Codec 是 Sonic JSON 编解码器.
//
// Cached data may be replaced with an oversized payload by a writer (or a
// compromised Redis), so the size is capped before decoding, or a single entry
// could claim a large amount of memory. MaxBytes of 0 uses 16 MiB; it must be
// nonnegative.
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

// Marshal encodes v as Sonic JSON | Marshal 把 v 编码为 Sonic JSON.
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

// Unmarshal decodes Sonic JSON within the size limit | Unmarshal 在大小限制内解码 Sonic JSON.
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
