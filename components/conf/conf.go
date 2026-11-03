// Package conf loads multi-file configuration on top of Viper, with both an instance API and a process-wide global one | conf 基于 Viper 加载多文件配置，提供实例 API 与进程级全局 API 两套用法.
//
// # Notes | 说明
//
// Config aggregates several file sources; each file is cached under its base
// name (extension stripped) as a *V (an alias of viper.Viper) with automatic
// environment overrides ("." in a key maps to "_").
// Config 聚合多个文件来源；每个文件按去掉扩展名的主名缓存为 *V（viper.Viper 的
// 别名），并开启环境变量覆盖（键中的 "." 映射为 "_"）。
//
// Init is a one-shot bootstrap: it loads the file named by CONFIG_PATH, or
// every .yaml/.yml/.json/.toml file in ./config. Until a config is loaded,
// Get/Set/Watch are silent no-ops and Scan/UnmarshalKey return
// ErrConfigFileNotFound. The global shortcuts operate on the default file
// "config" only.
// Init 是一次性引导：加载 CONFIG_PATH 指向的文件，或 ./config 目录下全部
// .yaml/.yml/.json/.toml 文件。配置加载前 Get/Set/Watch 静默空操作，
// Scan/UnmarshalKey 返回 ErrConfigFileNotFound。全局快捷入口只作用于默认
// 文件 "config"。
//
// # Features | 功能
//
// - Instance API: New/Load/File/Scan/Get/Set/Watch per file | 实例 API：按文件 New/Load/File/Scan/Get/Set/Watch
// - Global API: Init, LoadConfig, Get, Set, Scan, UnmarshalKey, Watch | 全局 API：Init、LoadConfig、Get、Set、Scan、UnmarshalKey、Watch
// - File watching via fsnotify on already-loaded files | 基于 fsnotify 监听已加载的文件
//
// # Example | 示例
//
//	if err := conf.Init(); err != nil {
//		log.Fatal(err)
//	}
//	name := conf.Get("app.name")
//	var cfg DBConfig
//	err := conf.UnmarshalKey("db", &cfg)
package conf

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/apus-run/gala/components/conf/file"
)

var _ Conf = (*Config)(nil)

// Config implements Conf over a set of file sources, caching each loaded
// file under its base name | Config 基于一组文件源实现 Conf，
// 每个加载的文件按主名缓存。
type Config struct {
	files  []file.Source
	cached *sync.Map
}

// New creates a Config from the given file sources | New 根据给定的文件源创建 Config。
func New(files []file.Source) *Config {
	return &Config{
		files:  files,
		cached: &sync.Map{},
	}
}

// Watch calls fn whenever any already loaded file changes
// | Watch 在任意已加载文件变更时调用 fn。
func (c *Config) Watch(fn func()) {
	c.cached.Range(func(key, value any) bool {
		v := value.(*V)
		v.OnConfigChange(func(e fsnotify.Event) {
			fn()
		})
		v.WatchConfig()
		return true
	})
}

// Scan unmarshals the named file into obj, returning ErrConfigFileNotFound
// when the file is not loaded | Scan 将指定文件反序列化到 obj，
// 文件未加载时返回 ErrConfigFileNotFound。
func (c *Config) Scan(filename string, obj any) error {
	file := c.File(filename)
	if file == nil {
		return ErrConfigFileNotFound
	}
	return file.Unmarshal(obj)
}

// UnmarshalKey unmarshals key from the default file into obj, returning
// ErrConfigFileNotFound when the file is not loaded
// | UnmarshalKey 将默认文件中的 key 反序列化到 obj，
// 默认文件未加载时返回 ErrConfigFileNotFound。
func (c *Config) UnmarshalKey(key string, obj any) error {
	file := c.File(defaultFile)
	if file == nil {
		return ErrConfigFileNotFound
	}
	return file.UnmarshalKey(key, obj)
}

// File returns the *V cached under filename, or nil when it is not loaded
// | File 返回按 filename 缓存的 *V，未加载时返回 nil。
func (c *Config) File(filename string) *V {
	if v, ok := c.cached.Load(filename); ok {
		return v.(*V)
	}
	return nil
}

// Get returns the value of key from the named file, or nil when the file
// is not loaded | Get 返回指定文件中 key 的值，文件未加载时返回 nil。
func (c *Config) Get(filename string, key string) any {
	file := c.File(filename)
	if file == nil {
		return nil
	}
	return file.Get(key)
}

// Set sets key in the named file; it is a no-op when the file is not loaded
// | Set 在指定文件中设置 key，文件未加载时为空操作。
func (c *Config) Set(filename string, key string, val any) {
	file := c.File(filename)
	if file == nil {
		return
	}
	file.Set(key, val)
}

// Load loads all sources and caches each file under its base name as a *V
// with automatic environment overrides; a missing file yields
// ErrConfigFileNotFound | Load 加载全部文件源，把每个文件按主名缓存为
// 开启环境变量覆盖的 *V；文件缺失时返回 ErrConfigFileNotFound。
func (c *Config) Load() error {
	if len(c.files) == 0 {
		return nil
	}
	for _, file := range c.files {
		kvs, err := file.Load()
		if err != nil {
			return err
		}

		for _, kv := range kvs {
			v := viper.New()
			v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
			v.AutomaticEnv()
			v.SetConfigType(kv.Format)
			v.SetConfigFile(kv.Path)

			if err := v.ReadInConfig(); err != nil {
				if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); ok {
					return ErrConfigFileNotFound
				}
				return err
			}

			name := strings.TrimSuffix(path.Base(kv.Key), filepath.Ext(kv.Key))
			c.cached.Store(name, v)
		}
	}
	return nil
}
