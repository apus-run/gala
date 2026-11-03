// Package mem provides memory-related helpers such as deep copy via gob.
// 包 mem 提供内存相关助手，如基于 gob 的深拷贝。
package mem

import (
	"bytes"
	"encoding/gob"
)

// DeepCopy deep-copies src into dst by gob-encoding and decoding.
// DeepCopy 通过 gob 编码再解码，把 src 深拷贝到 dst。
func DeepCopy(src, dst any) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(src); err != nil {
		return err
	}
	return gob.NewDecoder(bytes.NewBuffer(buf.Bytes())).Decode(dst)
}
