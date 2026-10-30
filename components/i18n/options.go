package i18n

import (
	"embed"

	"golang.org/x/text/language"
)

// Option is i18n option.
type Option interface {
	apply(*Options)
}

// OptionFunc adapts a plain function to Option.
type OptionFunc func(*Options)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// Options is i18n options.
type Options struct {
	format   string
	language language.Tag
	files    []string
	fs       embed.FS
}

// DefaultOptions .
func DefaultOptions() *Options {
	return &Options{
		format:   "yml",
		language: language.English,
		files:    []string{},
	}
}

// NewOptions 从 DefaultOptions 出发，按顺序应用 opts。
func NewOptions(opts ...Option) *Options {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply 按顺序原地应用 opts，不重置默认值、不做校验。
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

func WithFormat(format string) Option {
	return OptionFunc(func(o *Options) {
		if format != "" {
			o.format = format
		}
	})
}

func WithLanguage(lang language.Tag) Option {
	return OptionFunc(func(o *Options) {
		if lang.String() != "und" {
			o.language = lang
		}
	})
}

func WithFiles(files ...string) Option {
	return OptionFunc(func(o *Options) {
		if len(files) > 0 {
			o.files = files
		}
	})
}

func WithFile(file string) Option {
	return OptionFunc(func(o *Options) {
		if file != "" {
			o.files = append(o.files, file)
		}
	})
}

func WithFS(fs embed.FS) Option {
	return OptionFunc(func(o *Options) {
		o.fs = fs
	})
}
