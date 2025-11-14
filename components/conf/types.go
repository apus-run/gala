package conf

import "github.com/spf13/viper"

// V Viper 别名
type V = viper.Viper

type Conf interface {
	File(filename string) *V
	Scan(filename string, obj any) error
	Get(filename string, key string) any
	Set(filename string, key string, val any)
	Load() error
	Watch(fn func())
}
