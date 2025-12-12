//nolint:errcheck
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

// I18n is used to store the options and configurations for internationalization.
type I18n struct {
	opts      Options
	bundle    *i18n.Bundle
	localizer *i18n.Localizer
	lang      language.Tag
}

// New creates a new instance of the I18n struct with the given options.
// It takes a variadic parameter of functional options and returns a pointer to the I18n struct.
func New(options ...Option) *I18n {
	opts := Apply(options...)

	bundle := i18n.NewBundle(opts.language)
	localizer := i18n.NewLocalizer(bundle, opts.language.String())
	switch opts.format {
	case "toml":
		bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	case "json":
		bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	default:
		bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	}
	rp := &I18n{
		opts:      *opts,
		bundle:    bundle,
		localizer: localizer,
		lang:      opts.language,
	}
	for _, item := range opts.files {
		rp.Add(item)
	}
	rp.AddFS(opts.fs)
	return rp
}

// Select can change language.
func (i *I18n) Select(lang language.Tag) *I18n {
	if lang.String() == "und" {
		lang = i.opts.language
	}
	return &I18n{
		opts:      i.opts,
		bundle:    i.bundle,
		localizer: i18n.NewLocalizer(i.bundle, lang.String()),
		lang:      lang,
	}
}

// Language get current language.
func (i *I18n) Language() language.Tag {
	return i.lang
}

// LocalizeT localizes the given message and returns the localized string.
// If unable to translate, it returns the message ID as the default message.
func (i *I18n) LocalizeT(message *i18n.Message) (rp string) {
	if message == nil {
		return ""
	}

	var err error
	rp, err = i.localizer.Localize(&i18n.LocalizeConfig{
		DefaultMessage: message,
	})
	if err != nil {
		// use id as default message when unable to translate
		rp = message.ID
	}
	return
}

// LocalizeE is a wrapper for LocalizeT method that converts the localized string to an error type and returns it.
func (i *I18n) LocalizeE(message *i18n.Message) error {
	return errors.New(i.LocalizeT(message))
}

// T localizes the message with the given ID and returns the localized string.
// It uses the LocalizeT method to perform the translation.
func (i *I18n) T(id string) (rp string) {
	return i.LocalizeT(&i18n.Message{ID: id})
}

// E is a wrapper for T that converts the localized string to an error type and returns it.
func (i *I18n) E(id string) error {
	return errors.New(i.T(id))
}

// Add is add language file or dir(auto get language by filename).
func (i *I18n) Add(f string) {
	info, err := os.Stat(f)
	if err != nil {
		return
	}
	if info.IsDir() {
		filepath.Walk(f, func(path string, fi os.FileInfo, errBack error) (err error) {
			if !fi.IsDir() {
				_, err := i.bundle.LoadMessageFile(path)
				if err != nil {
					return err
				}
			}
			return
		})
	} else {
		_, err := i.bundle.LoadMessageFile(f)
		if err != nil {
			return
		}
	}
}

// AddFS is add language embed files.
func (i *I18n) AddFS(fs embed.FS) {
	files := readFS(fs, ".")
	for _, name := range files {
		i.bundle.LoadMessageFileFS(fs, name)
	}
}

func readFS(fs embed.FS, dir string) (rp []string) {
	rp = make([]string, 0)
	dirs, err := fs.ReadDir(dir)
	if err != nil {
		return
	}
	for _, item := range dirs {
		name := dir + string(os.PathSeparator) + item.Name()
		if dir == "." {
			name = item.Name()
		}
		if item.IsDir() {
			rp = append(rp, readFS(fs, name)...)
		} else {
			rp = append(rp, name)
		}
	}
	return
}
