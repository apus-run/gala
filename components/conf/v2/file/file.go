// Package file provides a conf.Source that reads one configuration file and
// decodes it with a codec registered in gala's pkg/encoding.
//
// Package file 提供读取单个配置文件的 conf.Source，用 gala pkg/encoding 中
// 已注册的 codec 解码。
package file

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/apus-run/gala/pkg/encoding"

	"github.com/apus-run/gala/components/conf/v2"
)

// Source reads a single file. The path is explicit; nothing is scanned or
// defaulted.
//
// Source 读取单个文件。路径须显式给出，不扫描目录，不设默认值。
type Source struct {
	path string
}

// New returns a Source for path. The codec is looked up by extension at Load
// time; the caller must import the matching codec package.
//
// New 返回读取 path 的来源。codec 在 Load 时按扩展名查找；调用方须导入对应的
// codec 包。
func New(path string) *Source {
	return &Source{path: path}
}

// SafeName returns the basename, which is the only file identity the library
// exposes in errors.
// SafeName 返回 basename，这是错误中唯一暴露的文件标识。
func (s *Source) SafeName() string {
	return filepath.Base(s.path)
}

// Load reads and decodes the file. Each call reads the file once and returns
// data the caller owns.
//
// Load 读取并解码文件。每次调用只读一次文件，返回调用方独有的数据。
func (s *Source) Load(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, s.failure(conf.StageCanceled, err)
	}
	ext := normalizeExt(s.path)
	codec := encoding.GetCodec(ext)
	if codec == nil {
		// A missing codec is a composition error: the app did not import it.
		// It is reported as a decode-stage failure so the cause is explicit.
		// 缺少 codec 是组合错误：应用未导入它。以 decode 阶段报告，使原因明确。
		return nil, s.failure(conf.StageDecode, nil)
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, s.wrapRead(err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		// An empty file is an explicit error, never an empty config.
		// 空文件是明确错误，而不是空配置。
		return nil, s.failure(conf.StageDecode, nil)
	}

	var out any
	if err := codec.Unmarshal(raw, &out); err != nil {
		// codec error text may echo input; keep only the marker.
		// codec 的错误文本可能回显输入，只保留标记。
		return nil, s.failure(conf.StageDecode, nil)
	}
	root, ok := out.(map[string]any)
	if !ok {
		return nil, s.failure(conf.StageDecode, nil)
	}
	return root, nil
}

// wrapRead keeps only the not-exist sentinel; other OS errors can contain the
// full path, which must not be exposed.
// wrapRead 只保留“不存在”哨兵；其他系统错误可能含完整路径，不得暴露。
func (s *Source) wrapRead(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return s.failure(conf.StageRead, fs.ErrNotExist)
	}
	return s.failure(conf.StageRead, nil)
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
	return conf.SourceError(stage, s.SafeName(), "", cause)
}
