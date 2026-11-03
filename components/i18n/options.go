package i18n

import (
	"embed"

	"golang.org/x/text/language"
)

// Option is i18n option. | Option 是 i18n 配置选项。
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option.
// OptionFunc 把普通函数适配为 Option。
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options is i18n options. | Options 是 i18n 配置。
type Options struct {
	format   string
	language language.Tag
	files    []string
	fs       embed.FS
}

// DefaultOptions returns the package defaults | DefaultOptions 返回包默认值。
func DefaultOptions() *Options {
	return &Options{
		format:   "yml",
		language: language.English,
		files:    []string{},
	}
}

// NewOptions starts from DefaultOptions and applies opts in order
// | NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place in order, without resetting defaults or validating
// | Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithFormat sets the file format, keeping the default when empty
// | WithFormat 设置文件格式，为空时保留默认值。
func WithFormat(format string) Option {
	return OptionFunc(func(o *Options) {
		if format != "" {
			o.format = format
		}
	})
}

// WithLanguage sets the default language, ignoring the und tag
// | WithLanguage 设置默认语言，忽略 und 标签。
func WithLanguage(lang language.Tag) Option {
	return OptionFunc(func(o *Options) {
		if lang.String() != "und" {
			o.language = lang
		}
	})
}

// WithFiles replaces the language file list | WithFiles 替换语言文件列表。
func WithFiles(files ...string) Option {
	return OptionFunc(func(o *Options) {
		if len(files) > 0 {
			o.files = files
		}
	})
}

// WithFile appends one language file | WithFile 追加单个语言文件。
func WithFile(file string) Option {
	return OptionFunc(func(o *Options) {
		if file != "" {
			o.files = append(o.files, file)
		}
	})
}

// WithFS sets the embedded language files | WithFS 设置内嵌语言文件。
func WithFS(fs embed.FS) Option {
	return OptionFunc(func(o *Options) {
		o.fs = fs
	})
}
