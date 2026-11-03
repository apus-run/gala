package conf

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/apus-run/gala/components/conf/file"
)

// ErrConfigNotInitialized is returned when the global config has not been
// initialized | 全局配置尚未初始化时返回 ErrConfigNotInitialized。
var ErrConfigNotInitialized = errors.New("config not initialized")

// ErrConfigFileNotFound is returned when a config file is not found
// | 找不到配置文件时返回 ErrConfigFileNotFound。
var ErrConfigFileNotFound = errors.New("config file not found")

var (
	global      = &configAppliance{}
	defaultFile = "config"
	once        sync.Once
)

type configAppliance struct {
	mu   sync.RWMutex
	conf Conf
}

// Init initializes the global config manually | Init 手动初始化配置。
func Init() error {
	var err error
	once.Do(func() {
		// 1. the CONFIG_PATH environment variable
		// 1. 环境变量
		if path := os.Getenv("CONFIG_PATH"); path != "" {
			err = LoadConfig([]file.Source{file.NewSource(path)})
			return
		}

		// 2. the ./config directory
		// 2. config 目录
		if pathRoot, wdErr := os.Getwd(); wdErr == nil {
			configDir := filepath.Join(pathRoot, "config")
			if entries, readErr := os.ReadDir(configDir); readErr == nil {
				var sources []file.Source
				for _, entry := range entries {
					if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
						ext := strings.ToLower(filepath.Ext(entry.Name()))
						if ext == ".yaml" || ext == ".yml" || ext == ".json" || ext == ".toml" {
							sources = append(sources, file.NewSource(filepath.Join(configDir, entry.Name())))
						}
					}
				}
				if len(sources) > 0 {
					err = LoadConfig(sources)
					return
				}
			}
		}

		err = ErrConfigFileNotFound
	})
	return err
}

// LoadConfig builds a Config from sources and installs it as the global one
// | LoadConfig 用 sources 构造 Config 并安装为全局配置。
func LoadConfig(sources []file.Source) error {
	c := New(sources)
	if err := c.Load(); err != nil {
		return err
	}
	global.SetConfig(c)
	return nil
}

// SetConfig replaces the global config | SetConfig 替换全局配置。
func (a *configAppliance) SetConfig(in Conf) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conf = in
}

// GetConfig returns the global config, nil before the first load
// | GetConfig 返回全局配置，首次加载前为 nil。
func (a *configAppliance) GetConfig() Conf {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.conf
}

func getFile() *V {
	conf := global.GetConfig()
	if conf == nil {
		return nil
	}
	return conf.File(defaultFile)
}

// Get returns the value of key from the default file, or nil when the config
// is not loaded | Get 返回默认文件中 key 的值，配置未加载时返回 nil。
func Get(key string) any {
	if file := getFile(); file != nil {
		return file.Get(key)
	}
	return nil
}

// Set sets key in the default file; it is a no-op when the config is not
// loaded | Set 在默认文件中设置 key，配置未加载时为空操作。
func Set(key string, val any) {
	if file := getFile(); file != nil {
		file.Set(key, val)
	}
}

// UnmarshalKey unmarshals key from the default file into obj, returning
// ErrConfigFileNotFound when the config is not loaded
// | UnmarshalKey 将默认文件中的 key 反序列化到 obj，
// 配置未加载时返回 ErrConfigFileNotFound。
func UnmarshalKey(key string, obj any) error {
	if file := getFile(); file != nil {
		return file.UnmarshalKey(key, obj)
	}
	return ErrConfigFileNotFound
}

// Scan unmarshals the whole default file into obj, returning
// ErrConfigFileNotFound when the config is not loaded
// | Scan 将整个默认文件反序列化到 obj，配置未加载时返回 ErrConfigFileNotFound。
func Scan(obj any) error {
	if file := getFile(); file != nil {
		return file.Unmarshal(obj)
	}
	return ErrConfigFileNotFound
}

// Load reloads the global config, returning ErrConfigNotInitialized when
// Init was never called | Load 重新加载全局配置，
// 从未调用过 Init 时返回 ErrConfigNotInitialized。
func Load() error {
	conf := global.GetConfig()
	if conf == nil {
		return ErrConfigNotInitialized
	}
	return conf.Load()
}

// Watch calls fn whenever any loaded file changes; it is a no-op when the
// config is not loaded | Watch 在已加载文件变更时调用 fn，配置未加载时为空操作。
func Watch(fn func()) {
	conf := global.GetConfig()
	if conf != nil {
		conf.Watch(fn)
	}
}
