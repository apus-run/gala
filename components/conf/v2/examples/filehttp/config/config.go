// Package config demonstrates file loading with explicit legacy ENV fallback.
package config

import (
	"context"
	"errors"
	"os"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/env"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
)

// Config is the complete runtime configuration of filehttp.
// Config 是 filehttp 的完整运行配置。
type Config struct {
	Server struct {
		ListenAddr     string `conf:"listen_addr"`
		MaxUploadBytes int64  `conf:"max_upload_bytes"`
	} `conf:"server"`
	Database struct {
		DSN string `conf:"dsn"`
	} `conf:"database"`
	Storage struct {
		FSRoot         string `conf:"fs_root"`
		S3Bucket       string `conf:"s3_bucket"`
		S3Region       string `conf:"s3_region"`
		S3Endpoint     string `conf:"s3_endpoint"`
		S3Prefix       string `conf:"s3_prefix"`
		S3PathStyle    bool   `conf:"s3_path_style"`
		S3CreateBucket bool   `conf:"s3_create_bucket"`
	} `conf:"storage"`
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

// Load applies one ENV mapping, preferring a non-empty new name to its legacy name.
func Load(ctx context.Context, path string) (*conf.Config[Config], error) {
	bindings := map[string]string{
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
	}
	legacy := map[string]string{
		"SERVER_LISTEN_ADDR":       "LISTEN_ADDR",
		"SERVER_MAX_UPLOAD_BYTES":  "MAX_UPLOAD_BYTES",
		"DATABASE_DSN":             "DB_DSN",
		"STORAGE_FS_ROOT":          "FS_ROOT",
		"STORAGE_S3_BUCKET":        "S3_BUCKET",
		"STORAGE_S3_REGION":        "S3_REGION",
		"STORAGE_S3_ENDPOINT":      "S3_ENDPOINT",
		"STORAGE_S3_PREFIX":        "S3_PREFIX",
		"STORAGE_S3_PATH_STYLE":    "S3_PATH_STYLE",
		"STORAGE_S3_CREATE_BUCKET": "S3_CREATE_BUCKET",
	}
	lookup := func(name string) (string, bool) {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			return value, true
		}
		return os.LookupEnv(legacy[name])
	}
	return conf.Open(ctx, Config{}, file.New(path, file.JSON), env.NewWithLookup(bindings, lookup))
}
