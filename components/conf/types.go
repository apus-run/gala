package conf

import "github.com/spf13/viper"

// V is an alias of viper.Viper | V 是 viper.Viper 的别名。
type V = viper.Viper

// Conf is the configuration interface: per-file Get/Set/Scan plus loading
// and watching | Conf 是配置接口：按文件 Get/Set/Scan，外加加载与监听。
type Conf interface {
	// File returns the *V cached under filename, or nil when it is not loaded
	// | File 返回按 filename 缓存的 *V，未加载时返回 nil。
	File(filename string) *V
	// Scan unmarshals the named file into obj | Scan 将指定文件反序列化到 obj。
	Scan(filename string, obj any) error
	// Get returns the value of key from the named file
	// | Get 返回指定文件中 key 的值。
	Get(filename string, key string) any
	// Set sets key in the named file | Set 在指定文件中设置 key。
	Set(filename string, key string, val any)
	// Load loads all the file sources | Load 加载全部文件源。
	Load() error
	// Watch calls fn whenever any loaded file changes
	// | Watch 在任意已加载文件变更时调用 fn。
	Watch(fn func())
}
