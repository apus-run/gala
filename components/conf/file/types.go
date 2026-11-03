package file

// Source is a configuration source that yields KV entries
// | Source 是产出 KV 条目的配置源。
type Source interface {
	// Load returns all entries of the source | Load 返回该源的全部条目。
	Load() ([]*KV, error)
}

// KV is a single configuration key-value pair | KV 是一条配置键值对。
type KV struct {
	Key    string
	Value  []byte
	Format string
	Path   string
}

// Read implements io.Reader over the raw value | Read 基于 Value 实现 io.Reader。
func (k *KV) Read(p []byte) (n int, err error) {
	return copy(p, k.Value), nil
}
