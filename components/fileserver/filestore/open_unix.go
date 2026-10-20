//go:build unix

package filestore

import "syscall"

// nonblockFlag 让打开 FIFO 时不阻塞等待写者。对普通文件无影响；
// 打开后的 Stat 会把非常规文件判定为不存在。
const nonblockFlag = syscall.O_NONBLOCK
