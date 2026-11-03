// Package msgpack adapts JetCache's MessagePack + S2 threshold algorithm.
// See ../../LICENSE.jetcache-go and ../../NOTICE.md.
// msgpack 适配 JetCache 的 MessagePack + S2 阈值算法。
// 许可信息见 ../../LICENSE.jetcache-go 与 ../../NOTICE.md。
package msgpack

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/s2"
	"github.com/vmihailenco/msgpack/v5"
)

// Codec appends a compression discriminator to MessagePack payloads. Payloads
// shorter than 64 bytes are not compressed. MaxDecodedBytes bounds decompression;
// zero uses 16 MiB. It must be nonnegative.
// Codec 在 MessagePack 载荷后追加压缩判别字节。短于 64 字节的载荷不压缩。
// MaxDecodedBytes 限制解压大小；零值用 16 MiB；必须非负。
type Codec struct{ MaxDecodedBytes int }

func (c Codec) limit() (int, error) {
	if c.MaxDecodedBytes < 0 {
		return 0, errors.New("msgpack: invalid decoded size limit")
	}
	if c.MaxDecodedBytes == 0 {
		return 16 << 20, nil
	}
	return c.MaxDecodedBytes, nil
}

// Marshal encodes v as MessagePack, compressing large payloads | Marshal 把 v 编码为 MessagePack，大载荷压缩.
func (c Codec) Marshal(v any) ([]byte, error) {
	limit, err := c.limit()
	if err != nil {
		return nil, err
	}
	b, err := msgpack.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, errors.New("msgpack: payload exceeds decoded size limit")
	}
	if err := validate(b); err != nil {
		return nil, err
	}
	if len(b) < 64 {
		return append(b, 0), nil
	}
	return append(s2.Encode(nil, b), 1), nil
}

// Unmarshal decodes a payload produced by Marshal | Unmarshal 解码 Marshal 产出的载荷.
func (c Codec) Unmarshal(b []byte, v any) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("msgpack: decoder panic: %v", p)
		}
	}()
	limit, err := c.limit()
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return errors.New("msgpack: empty payload")
	}
	kind := b[len(b)-1]
	b = b[:len(b)-1]
	switch kind {
	case 0:
		if len(b) > limit {
			return errors.New("msgpack: payload exceeds decoded size limit")
		}
	case 1:
		n, err := s2.DecodedLen(b)
		if err != nil {
			return err
		}
		if n > limit {
			return errors.New("msgpack: decoded payload too large")
		}
		b, err = s2.Decode(nil, b)
		if err != nil {
			return err
		}
	default:
		return errors.New("msgpack: unknown compression")
	}
	starts, err := scan(b)
	if err != nil {
		return err
	}
	raw := bytes.NewReader(b)
	var reader io.Reader = raw
	if len(starts) > 0 {
		reader = &extensionReader{Reader: raw, starts: starts}
	}
	decoder := msgpack.GetDecoder()
	defer msgpack.PutDecoder(decoder)
	decoder.Reset(reader)
	return decoder.Decode(v)
}

// validate scans framing without allocating containers. The upstream decoder
// allocates interface maps/slices from declared lengths before reading their
// contents; a tiny forged header must not request an enormous allocation.
// validate 只扫描帧结构，不分配容器。上游解码器会按声明长度预先分配
// interface map/slice 再读内容；伪造的小头绝不能申请巨额分配。
func validate(b []byte) error { _, err := scan(b); return err }

func scan(b []byte) (map[int64]struct{}, error) {
	var starts map[int64]struct{}
	invalid := errFraming
	stack := []uint64{1}
	pos := 0
	for len(stack) > 0 {
		last := len(stack) - 1
		if stack[last] == 0 {
			stack = stack[:last]
			continue
		}
		stack[last]--
		if pos >= len(b) {
			return nil, invalid
		}
		code := b[pos]
		pos++
		var payload, children, extra uint64
		var width int
		container := false
		extension := false
		factor := uint64(1)
		switch {
		case code <= 0x7f || code >= 0xe0 || code == 0xc0 || code == 0xc2 || code == 0xc3:
		case code >= 0xa0 && code <= 0xbf:
			payload = uint64(code & 31)
		case code >= 0x90 && code <= 0x9f:
			children = uint64(code & 15)
		case code >= 0x80 && code <= 0x8f:
			children = 2 * uint64(code&15)
		default:
			switch code {
			case 0xc4, 0xd9:
				width = 1
			case 0xc5, 0xda:
				width = 2
			case 0xc6, 0xdb:
				width = 4
			case 0xc7:
				extension = true
				width = 1
				extra = 1
			case 0xc8:
				extension = true
				width = 2
				extra = 1
			case 0xc9:
				extension = true
				width = 4
				extra = 1
			case 0xcc, 0xd0:
				payload = 1
			case 0xcd, 0xd1:
				payload = 2
			case 0xca, 0xce, 0xd2:
				payload = 4
			case 0xcb, 0xcf, 0xd3:
				payload = 8
			case 0xd4:
				extension = true
				payload = 2
			case 0xd5:
				extension = true
				payload = 3
			case 0xd6:
				extension = true
				payload = 5
			case 0xd7:
				extension = true
				payload = 9
			case 0xd8:
				extension = true
				payload = 17
			case 0xdc:
				width = 2
				container = true
			case 0xdd:
				width = 4
				container = true
			case 0xde:
				width = 2
				container = true
				factor = 2
			case 0xdf:
				width = 4
				container = true
				factor = 2
			default:
				return nil, invalid
			}
		}
		if width > len(b)-pos {
			return nil, invalid
		}
		if width > 0 {
			var n uint64
			for range width {
				n = n<<8 | uint64(b[pos])
				pos++
			}
			if container {
				children = n * factor
			} else {
				payload = n + extra
			}
		}
		if payload > uint64(len(b)-pos) {
			return nil, invalid
		}
		if extension && payload > 1 {
			if starts == nil {
				starts = make(map[int64]struct{})
			}
			starts[int64(pos+1)] = struct{}{}
		}
		pos += int(payload)
		// Every child requires at least a tag byte. This rejects huge count headers
		// before invoking the allocating decoder, and bounds nesting to 64 levels.
		// 每个子项至少需要一个 tag 字节。这样会在调用会分配内存的解码器之前
		// 拒绝超大的计数头，并把嵌套限制在 64 层。
		if children > uint64(len(b)-pos) {
			return nil, invalid
		}
		if children > 0 {
			if len(stack) >= 65 {
				return nil, invalid
			}
			stack = append(stack, children)
		}
	}
	if pos != len(b) {
		return nil, invalid
	}
	return starts, nil
}

// vmihailenco DecodeMapLen skips an extension header and then reads its body
// as a map tag. Reading an opaque body's first byte as a new tag is forbidden;
// proper extension decoders (including time.Time) consume it with Read instead.
// The reader also implements ByteScanner, so upstream does not hide its offsets
// behind a bufio.Reader. No global decoder/extension registrations are modified.
// vmihailenco 的 DecodeMapLen 会跳过扩展头并把其 body 当作 map tag 读取。
// 禁止把不透明 body 的首字节当新 tag 读；规范的扩展解码器（含 time.Time）
// 改用 Read 消费它。该 reader 同时实现 ByteScanner，因此上游不会把偏移藏进
// bufio.Reader。不修改任何全局 decoder/extension 注册。
type extensionReader struct {
	*bytes.Reader
	starts map[int64]struct{}
}

// ReadByte rejects reads into opaque extension bodies | ReadByte 拒绝读取不透明扩展体的字节.
func (r *extensionReader) ReadByte() (byte, error) {
	pos := r.Size() - int64(r.Len())
	if _, opaque := r.starts[pos]; opaque {
		return 0, errExtensionBody
	}
	return r.Reader.ReadByte()
}

var errFraming = errors.New("msgpack: invalid framing or nesting")
var errExtensionBody = errors.New("msgpack: extension body cannot be decoded as a container")
