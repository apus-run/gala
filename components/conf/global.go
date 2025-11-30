package conf

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/apus-run/gala/components/conf/file"
)

var ErrConfigNotInitialized = errors.New("config not initialized")
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

// Init 手动初始化配置
func Init() error {
	var err error
	once.Do(func() {
		// 1. 环境变量
		if path := os.Getenv("CONFIG_PATH"); path != "" {
			err = LoadConfig([]file.Source{file.NewSource(path)})
			return
		}

		// 2. config目录
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

func LoadConfig(sources []file.Source) error {
	c := New(sources)
	if err := c.Load(); err != nil {
		return err
	}
	global.SetConfig(c)
	return nil
}

func (a *configAppliance) SetConfig(in Conf) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conf = in
}

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

func Get(key string) any {
	if file := getFile(); file != nil {
		return file.Get(key)
	}
	return nil
}

func Set(key string, val any) {
	if file := getFile(); file != nil {
		file.Set(key, val)
	}
}

func UnmarshalKey(key string, obj any) error {
	if file := getFile(); file != nil {
		return file.UnmarshalKey(key, obj)
	}
	return ErrConfigFileNotFound
}

func Scan(obj any) error {
	if file := getFile(); file != nil {
		return file.Unmarshal(obj)
	}
	return ErrConfigFileNotFound
}

func Load() error {
	conf := global.GetConfig()
	if conf == nil {
		return ErrConfigNotInitialized
	}
	return conf.Load()
}

func Watch(fn func()) {
	conf := global.GetConfig()
	if conf != nil {
		conf.Watch(fn)
	}
}
