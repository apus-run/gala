package i18n

import (
	"embed"

	"golang.org/x/text/language"
)

// Option is i18n option.
type Option func(*Options)

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

func Apply(opts ...Option) *Options {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

func WithFormat(format string) Option {
	return func(o *Options) {
		if format != "" {
			o.format = format
		}
	}
}

func WithLanguage(lang language.Tag) Option {
	return func(o *Options) {
		if lang.String() != "und" {
			o.language = lang
		}
	}
}

func WithFiles(files ...string) Option {
	return func(o *Options) {
		if len(files) > 0 {
			o.files = files
		}
	}
}

func WithFile(file string) Option {
	return func(o *Options) {
		if file != "" {
			o.files = append(o.files, file)
		}
	}
}

func WithFS(fs embed.FS) Option {
	return func(o *Options) {
		o.fs = fs
	}
}
