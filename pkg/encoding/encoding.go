// Package encoding defines the [Codec] interface and a global codec registry
// shared by transports | encoding 定义传输层共用的 [Codec] 接口与全局编解码器注册表.
//
// # Notes | 说明
//
// Codec implementations must be thread safe; their methods can be called
// from concurrent goroutines. [RegisterCodec] panics on a nil codec or an
// empty Name, and lowercases the name before storing it; [GetCodec] expects
// a lowercase content subtype and returns nil when nothing is registered
// under it. The codec subpackages (form, json, proto, protojson, xml, yaml)
// register themselves on import.
//
// Codec 实现必须并发安全，其方法可能被多个 goroutine 同时调用。
// [RegisterCodec] 对 nil 编解码器或空 Name 会 panic，注册前会把名称转为小写；
// [GetCodec] 要求小写的 content subtype，未注册时返回 nil。
// 各编解码子包（form、json、proto、protojson、xml、yaml）导入时自行注册。
package encoding

import (
	"strings"
	"sync"
)

// Codec defines the interface Transport uses to encode and decode messages. Note
// that implementations of this interface must be thread safe; a Codec's
// methods can be called from concurrent goroutines.
//
// Codec 定义 Transport 编解码消息所用的接口。实现必须线程安全：
// 其方法可能被多个 goroutine 并发调用。
type Codec interface {
	// Marshal returns the wire format of v | Marshal 返回 v 的传输格式.
	Marshal(v any) ([]byte, error)
	// Unmarshal parses the wire format into v | Unmarshal 将传输格式解析进 v.
	Unmarshal(data []byte, v any) error
	// Name returns the name of the Codec implementation. The returned string
	// will be used as part of content type in transmission. The result must be
	// static; the result cannot change between calls.
	// Name 返回编解码器实现名。返回值用作传输时 content type 的一部分，
	// 必须是静态的：多次调用之间不可变化。
	Name() string
}

var (
	registeredCodecsMu sync.RWMutex
	registeredCodecs   = make(map[string]Codec)
)

// RegisterCodec registers the provided Codec for use with all Transport clients and
// servers | RegisterCodec 注册编解码器，供所有 Transport 客户端与服务端使用.
//
// It panics on a nil codec or an empty Name result, and stores the codec
// under the lowercased name.
// codec 为 nil 或 Name 返回空串时 panic；注册名会转为小写存储。
func RegisterCodec(codec Codec) {
	if codec == nil {
		panic("cannot register a nil Codec")
	}
	name := codec.Name()
	if name == "" {
		panic("cannot register Codec with empty string result for Name()")
	}
	contentSubtype := strings.ToLower(name)
	registeredCodecsMu.Lock()
	defer registeredCodecsMu.Unlock()
	registeredCodecs[contentSubtype] = codec
}

// GetCodec gets a registered Codec by content-subtype, or nil if no Codec is
// registered for the content-subtype | GetCodec 按 content subtype 取已注册的
// 编解码器，未注册时返回 nil.
//
// The content-subtype is expected to be lowercase.
// content subtype 须为小写。
func GetCodec(contentSubtype string) Codec {
	registeredCodecsMu.RLock()
	defer registeredCodecsMu.RUnlock()
	return registeredCodecs[contentSubtype]
}
