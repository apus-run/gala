//go:build !unix

package filestore

// nonblockFlag 在该平台没有等价标志，打开 FIFO 的行为由平台决定。
const nonblockFlag = 0
