// Package file provides a conf.Source that reads one configuration file and
// decodes it with a codec registered in gala's pkg/encoding.
//
// Package file 提供读取单个配置文件的 conf.Source，用 gala pkg/encoding 中
// 已注册的 codec 解码。
package file

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/apus-run/gala/pkg/encoding"

	"github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/internal/diagnostic"
	"github.com/apus-run/gala/components/conf/v2/internal/document"
)

// Source reads a single file. The path is explicit; nothing is scanned or
// defaulted.
//
// Source 读取单个文件。路径须显式给出，不扫描目录，不设默认值。
type Source struct {
	path  string
	fsys  fs.FS
	useFS bool
}

// New returns a Source for path. The codec is looked up by extension at Load
// time; the caller must import the matching codec package.
//
// New 返回读取 path 的来源。codec 在 Load 时按扩展名查找；调用方须导入对应的
// codec 包。
func New(path string) *Source {
	return &Source{path: path}
}

// NewFS reads one valid, non-root io/fs path. Invalid arguments fail on Load.
func NewFS(fsys fs.FS, name string) *Source {
	return &Source{path: name, fsys: fsys, useFS: true}
}

// Load reads and decodes the file. Each call reads the file once and returns
// data the caller owns.
//
// Load 读取并解码文件。每次调用只读一次文件，返回调用方独有的数据。
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, s.failure(conf.StageCanceled, err)
	}
	if s.useFS && (s.fsys == nil || !fs.ValidPath(s.path) || s.path == ".") {
		return nil, s.failure(conf.StageConfig, nil)
	}
	ext := normalizeExt(s.path)
	codec := encoding.GetCodec(ext)
	if codec == nil {
		// A missing codec is a composition error: the app did not import it.
		// It is reported as a decode-stage failure so the cause is explicit.
		// 缺少 codec 是组合错误：应用未导入它。以 decode 阶段报告，使原因明确。
		return nil, s.failure(conf.StageDecode, diagnostic.CodecMissing)
	}

	raw, err := s.ReadBytes(ctx)
	if err != nil {
		return nil, err
	}
	return document.Decode(codec, raw)
}

// ReadBytes reads the file without choosing a codec, for document composition.
func (s *Source) ReadBytes(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.useFS && (s.fsys == nil || !fs.ValidPath(s.path) || s.path == ".") {
		return nil, s.failure(conf.StageConfig, nil)
	}
	var raw []byte
	var err error
	if s.useFS {
		raw, err = fs.ReadFile(s.fsys, s.path)
	} else {
		raw, err = os.ReadFile(s.path)
	}
	if cause := ctx.Err(); cause != nil {
		return nil, cause
	}
	if err != nil {
		return nil, s.failure(conf.StageRead, err)
	}
	return raw, nil
}

// normalizeExt lowercases the extension and maps "yml" to "yaml".
// normalizeExt 把扩展名转为小写，并把 yml 归一为 yaml。
func normalizeExt(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "yml" {
		return "yaml"
	}
	return ext
}

func (s *Source) failure(stage conf.Stage, cause error) error {
	return conf.SourceError(stage, "", "", cause)
}
