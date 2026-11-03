//go:build unix

package filestore

import "syscall"

// nonblockFlag makes opening a FIFO not block waiting for a writer. It has no
// effect on regular files; the Stat after opening classifies non-regular
// files as missing.
// nonblockFlag 让打开 FIFO 时不阻塞等待写者。对普通文件无影响；
// 打开后的 Stat 会把非常规文件判定为不存在。
const nonblockFlag = syscall.O_NONBLOCK
