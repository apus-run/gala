package filestore

import "io/fs"

// 默认值集中定义，便于测试与文档引用。
const (
	defaultBufferSize = 256 << 10 // 拷贝缓冲：256 KiB
	maxBufferSize     = 4 << 20   // 拷贝缓冲上限：4 MiB

	defaultFileMode fs.FileMode = 0o644
	defaultDirMode  fs.FileMode = 0o755
	tempFileMode    fs.FileMode = 0o600 // 临时文件在发布前保持 0600，避免半成品被他人读取

	// dataDirName 是根目录内的数据区。Key 只映射到这里，临时区在另一个目录，
	// 两者的命名空间天然不相交，Key 规则不必知道后端的内部布局。
	dataDirName = "data"
	// tempDirName 是根目录内的临时区，与数据位于同一文件系统，保证 rename 原子。
	tempDirName = ".fs-tmp"
	// maxTempAttempts 限制临时文件名冲突时的重试次数。
	maxTempAttempts = 8
)

func defaultConfig() config {
	return config{
		durability: SyncFileAndDir,
		fileMode:   defaultFileMode,
		dirMode:    defaultDirMode,
		bufferSize: defaultBufferSize,
	}
}
