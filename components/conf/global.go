package conf

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/pflag"

	"github.com/apus-run/gala/components/conf/file"
)

var ErrConfigNotInitialized = errors.New("config not initialized")
var ErrConfigFileNotFound = errors.New("config file not found")

var (
	global      = &configAppliance{}
	defaultFile = "config"
	initialized = false
)

type configAppliance struct {
	mu   sync.RWMutex
	conf Conf
}

func init() {
	// 可以通过环境变量禁用自动初始化
	if os.Getenv("CONFIG_AUTO_INIT") == "false" {
		return
	}

	if err := initializeConfig(); err != nil {
		log.Printf("config init failed: %v", err)
	} else {
		initialized = true
	}
}

// Init 手动初始化配置
func Init() error {
	if initialized {
		return nil
	}

	if err := initializeConfig(); err != nil {
		return err
	}

	initialized = true
	return nil
}

func initializeConfig() error {
	// 1. 环境变量
	if path := os.Getenv("CONFIG_PATH"); path != "" {
		return loadConfig([]file.Source{file.NewSource(path)})
	}

	// 2. 命令行参数
	var cmdPath string
	pflag.StringVarP(&cmdPath, "conf", "c", "config/config.yaml", "config path")
	pflag.Parse()
	if cmdPath != "" {
		return loadConfig([]file.Source{file.NewSource(cmdPath)})
	}

	// 3. config目录
	if pathRoot, err := os.Getwd(); err == nil {
		configDir := filepath.Join(pathRoot, "config")
		if entries, err := os.ReadDir(configDir); err == nil {
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
				return loadConfig(sources)
			}
		}
	}

	return ErrConfigFileNotFound
}

func loadConfig(sources []file.Source) error {
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
