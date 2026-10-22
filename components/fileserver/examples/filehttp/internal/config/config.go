// Package config 用 components/conf 加载 filehttp 的配置。
//
// 配置来源是一个 YAML/JSON/TOML 文件（默认 config/config.yaml）。conf 会为
// 每个配置键启用 AutomaticEnv，因此 STORAGE_S3_BUCKET 这类“路径转下划线”的
// 环境变量可以覆盖文件值；为了兼容既有 Docker 运行方式，本包同时把旧变量名
// （S3_BUCKET、LISTEN_ADDR 等）显式绑定到同一组键。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apus-run/gala/components/conf"
	"github.com/apus-run/gala/components/conf/file"
)

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

// Load 读取 CONFIG_PATH（默认 config/config.yaml）并应用环境变量覆盖。
func Load() (Config, error) {
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = filepath.Join("config", "config.yaml")
	}
	c := conf.New([]file.Source{file.NewSource(path)})
	if err := c.Load(); err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", path, err)
	}
	v := c.File(configName(path))
	if v == nil {
		return Config{}, fmt.Errorf("config %q: no file named %q", path, configName(path))
	}

	bindLegacyEnv(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}
	if cfg.Server.ListenAddr == "" {
		return Config{}, fmt.Errorf("config: server.listen_addr is required")
	}
	if cfg.Server.MaxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("config: server.max_upload_bytes must be positive")
	}
	if cfg.Database.DSN == "" {
		return Config{}, fmt.Errorf("config: database.dsn is required")
	}
	if cfg.Storage.FSRoot == "" {
		return Config{}, fmt.Errorf("config: storage.fs_root is required")
	}
	if cfg.Storage.S3Region == "" {
		cfg.Storage.S3Region = "us-east-1"
	}
	return cfg, nil
}

// configName 返回 conf 为配置文件生成的缓存名。目录来源按 config.* 命名；
// 单文件来源使用去掉扩展名的文件名。
func configName(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return "config"
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// bindLegacyEnv 保留既有环境变量名。新名字（如 STORAGE_S3_BUCKET）由 conf.Load
// 的 AutomaticEnv 自动支持；显式绑定只是旧名字的兼容层。
func bindLegacyEnv(v *conf.V) {
	aliases := map[string]string{
		"server.listen_addr":       "LISTEN_ADDR",
		"server.max_upload_bytes":  "MAX_UPLOAD_BYTES",
		"database.dsn":             "DB_DSN",
		"storage.fs_root":          "FS_ROOT",
		"storage.s3_bucket":        "S3_BUCKET",
		"storage.s3_region":        "S3_REGION",
		"storage.s3_endpoint":      "S3_ENDPOINT",
		"storage.s3_prefix":        "S3_PREFIX",
		"storage.s3_path_style":    "S3_PATH_STYLE",
		"storage.s3_create_bucket": "S3_CREATE_BUCKET",
	}
	for key, env := range aliases {
		_ = v.BindEnv(key, env)
	}
}
