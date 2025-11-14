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

type Config struct {
	files  []file.Source
	cached *sync.Map
}

func New(files []file.Source) *Config {
	return &Config{
		files:  files,
		cached: &sync.Map{},
	}
}

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

func (c *Config) Scan(filename string, obj any) error {
	file := c.File(filename)
	if file == nil {
		return ErrConfigFileNotFound
	}
	return file.Unmarshal(obj)
}

func (c *Config) UnmarshalKey(key string, obj any) error {
	file := c.File(defaultFile)
	if file == nil {
		return ErrConfigFileNotFound
	}
	return file.UnmarshalKey(key, obj)
}

func (c *Config) File(filename string) *V {
	if v, ok := c.cached.Load(filename); ok {
		return v.(*V)
	}
	return nil
}

func (c *Config) Get(filename string, key string) any {
	file := c.File(filename)
	if file == nil {
		return nil
	}
	return file.Get(key)
}

func (c *Config) Set(filename string, key string, val any) {
	file := c.File(filename)
	if file == nil {
		return
	}
	file.Set(key, val)
}

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
			v.SetConfigType(kv.Format)
			v.SetConfigFile(kv.Path)

			if err := v.ReadInConfig(); err != nil {
				var configFileNotFoundError viper.ConfigFileNotFoundError
				if errors.As(err, &configFileNotFoundError) {
					return ErrConfigFileNotFound
				}
				return err
			}
			v.AutomaticEnv()

			name := strings.TrimSuffix(path.Base(kv.Key), filepath.Ext(kv.Key))
			c.cached.Store(name, v)
		}
	}
	return nil
}
