// Package options defines the Option interface that gala components follow
// for flag-driven configuration structs | options 包定义各组件旗帜驱动配置结构
// 遵循的 Option 接口.
//
// # Notes | 说明
//
// The package contains only the interface; each component ships its own
// implementing struct. Validate should aggregate every problem into one error
// (e.g. errors.Join) instead of failing on the first, and may complete unset
// fields. AddFlags registers the struct's pflag flags, where the optional
// prefixes disambiguate repeated components (two servers each needing an
// address flag, say) inside one FlagSet.
// 本包只包含接口，各组件自行提供实现结构体。Validate 应把所有问题聚合为一个错误
// （如 errors.Join）而不是遇到第一个就返回，也可以补全未设置的字段。AddFlags 注册
// 该结构体的 pflag 旗帜，可选的 prefixes 用于在同一个 FlagSet 里区分重复组件
// （比如两个各自需要 address 旗帜的服务）。
package options

import "github.com/spf13/pflag"

// Option defines methods to implement a generic option | Option 定义实现通用
// 选项需要的方法.
type Option interface {
	// Validate validates all the required option. It can also be used to
	// complete option if needed. If there are multiple errors, it should
	// return a single error that contains all the errors, for example using
	// errors.Join from Go 1.20:
	// errs := []error
	// if err := o.Validate(); err != nil {
	//    errs = append(errs, err)
	// }
	// if err := o.Complete(); err != nil {
	//    errs = append(errs, err)
	// }
	// if len(errs) > 0 {
	// 	  return errors.Join(errs...)
	// }
	// If the option is not valid, it should return an error; if the option
	// is valid, it should return nil.
	//
	// Validate 校验所有必填选项，也可用于按需补全选项。存在多个错误时应返回
	// 一个包含全部错误的单个错误，例如用 Go 1.20 的 errors.Join
	// （见上方示例代码）。选项不合法时返回错误，合法时返回 nil。
	Validate() error

	// AddFlags adds flags related to given flag | AddFlags 向给定的
	// FlagSet 添加相关旗帜.
	AddFlags(fs *pflag.FlagSet, prefixes ...string)
}
