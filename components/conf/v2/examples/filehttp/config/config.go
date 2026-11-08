// Package config loads filehttp's configuration with components/conf/v2.
//
// It is the migration sample for spec §8: one file source, the legacy and new
// environment names as explicit ordered ENV sources (legacy first, so the new
// name wins when both are set), and a Validate that keeps the old required
// fields. Unlike the legacy loader it does not fill s3_region when empty: v2's
// Validate may only check values, so the default lives in the config file.
//
// Package config 用 components/conf/v2 加载 filehttp 的配置。
//
// 这是规格第 8 节的迁移样例：一个文件来源；新旧环境变量名作为显式、有序的
// ENV 来源（旧名在前，两者都设置时新名胜出）；Validate 保留旧版的必填项。
// 与旧加载器不同，s3_region 为空时不会自动填默认值：v2 的 Validate 只能检查值，
// 默认值改由配置文件提供。
package config

import (
	"context"
	"errors"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/env"
	"github.com/apus-run/gala/components/conf/v2/file"

	// Codecs register on import; this composition root imports the one it uses.
	// codec 在导入时注册；此组合入口导入所用的 codec。
	_ "github.com/apus-run/gala/pkg/encoding/yaml"
)

// Config is the complete runtime configuration of filehttp.
// Config 是 filehttp 的完整运行配置。
type Config struct {
	Server struct {
		ListenAddr     string `mapstructure:"listen_addr"`
		MaxUploadBytes int64  `mapstructure:"max_upload_bytes"`
	} `mapstructure:"server"`
	Database struct {
		DSN string `mapstructure:"dsn"`
	} `mapstructure:"database"`
	Storage struct {
		FSRoot         string `mapstructure:"fs_root"`
		S3Bucket       string `mapstructure:"s3_bucket"`
		S3Region       string `mapstructure:"s3_region"`
		S3Endpoint     string `mapstructure:"s3_endpoint"`
		S3Prefix       string `mapstructure:"s3_prefix"`
		S3PathStyle    bool   `mapstructure:"s3_path_style"`
		S3CreateBucket bool   `mapstructure:"s3_create_bucket"`
	} `mapstructure:"storage"`
}

// Validate keeps the required-field rules of the legacy loader.
// Validate 保留旧加载器的必填项规则。
func (c *Config) Validate() error {
	if c.Server.ListenAddr == "" {
		return errors.New("config: server.listen_addr is required")
	}
	if c.Server.MaxUploadBytes <= 0 {
		return errors.New("config: server.max_upload_bytes must be positive")
	}
	if c.Database.DSN == "" {
		return errors.New("config: database.dsn is required")
	}
	if c.Storage.FSRoot == "" {
		return errors.New("config: storage.fs_root is required")
	}
	return nil
}

// legacyEnv maps the names the Docker setup already uses. It is ordered before
// the new names so that, when both are set, the new name wins (spec §8).
// legacyEnv 映射 Docker 中已在使用的旧变量名。它排在新名之前，使两者都设置时
// 新名胜出（规格第 8 节）。
func legacyEnv() conf.Source {
	return env.New(map[string]string{
		"LISTEN_ADDR":      "server.listen_addr",
		"MAX_UPLOAD_BYTES": "server.max_upload_bytes",
		"DB_DSN":           "database.dsn",
		"FS_ROOT":          "storage.fs_root",
		"S3_BUCKET":        "storage.s3_bucket",
		"S3_REGION":        "storage.s3_region",
		"S3_ENDPOINT":      "storage.s3_endpoint",
		"S3_PREFIX":        "storage.s3_prefix",
		"S3_PATH_STYLE":    "storage.s3_path_style",
		"S3_CREATE_BUCKET": "storage.s3_create_bucket",
	})
}

// newEnv maps the STORAGE_* / SERVER_* / DATABASE_* names that the legacy
// AutomaticEnv accepted.
// newEnv 映射旧版 AutomaticEnv 接受的 STORAGE_*、SERVER_*、DATABASE_* 名。
func newEnv() conf.Source {
	return env.New(map[string]string{
		"SERVER_LISTEN_ADDR":       "server.listen_addr",
		"SERVER_MAX_UPLOAD_BYTES":  "server.max_upload_bytes",
		"DATABASE_DSN":             "database.dsn",
		"STORAGE_FS_ROOT":          "storage.fs_root",
		"STORAGE_S3_BUCKET":        "storage.s3_bucket",
		"STORAGE_S3_REGION":        "storage.s3_region",
		"STORAGE_S3_ENDPOINT":      "storage.s3_endpoint",
		"STORAGE_S3_PREFIX":        "storage.s3_prefix",
		"STORAGE_S3_PATH_STYLE":    "storage.s3_path_style",
		"STORAGE_S3_CREATE_BUCKET": "storage.s3_create_bucket",
	})
}

// Load reads the config file at path and applies the environment layers in
// order: file, then legacy names, then new names.
// Load 读取 path 处的配置文件，并按顺序叠加环境变量层：文件、旧名、新名。
func Load(ctx context.Context, path string) (*conf.Config[Config], error) {
	return conf.Open[Config](ctx, file.New(path), legacyEnv(), newEnv())
}
