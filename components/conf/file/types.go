package file

type Source interface {
	Load() ([]*KV, error)
}

// KV KeyValue is conf key value.
type KV struct {
	Key    string
	Value  []byte
	Format string
	Path   string
}

func (k *KV) Read(p []byte) (n int, err error) {
	return copy(p, k.Value), nil
}
