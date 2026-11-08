package conf_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/env"
	"github.com/apus-run/gala/components/conf/v2/file"

	// Codecs register on import; the composition root must import them.
	// codec 在导入时注册；组合入口必须导入它们。
	_ "github.com/apus-run/gala/pkg/encoding/yaml"
)

// filehttpConfig mirrors the fields of filehttp's Config that the migration
// binds. server.mode is intentionally absent: the spec removes it before
// migration, so strict binding must reject it.
// filehttpConfig 对应 filehttp 迁移需要绑定的字段。server.mode 有意缺席：
// 规格要求迁移前删除它，因此严格绑定必须拒绝它。
type filehttpConfig struct {
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

// filehttpEnv is the migration mapping from the spec. Legacy names come first
// so the new names, listed second, win when both are set (C08).
// filehttpEnv 是规格中的迁移映射。旧名在前、新名在后，两者都设置时新名胜出（C08）。
func filehttpEnv() []conf.Source {
	legacy := env.New(map[string]string{
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
	modern := env.New(map[string]string{
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
	return []conf.Source{legacy, modern}
}

// filehttpYAML is the real filehttp config with server.mode removed, as the
// migration requires.
// filehttpYAML 是删除 server.mode 后的真实 filehttp 配置，即迁移所需的形式。
const filehttpYAML = `server:
  listen_addr: ":8080"
  max_upload_bytes: 524288000

database:
  dsn: "./data/filehost.db"

storage:
  fs_root: "./data"
  s3_bucket: ""
  s3_region: "us-east-1"
  s3_endpoint: ""
  s3_prefix: ""
  s3_path_style: true
  s3_create_bucket: false
`

// C08: with server.mode present, strict binding fails; removing it passes.
// C08：存在 server.mode 时严格绑定失败；删除后通过。
func TestC08ServerModeIsRejectedUntilRemoved(t *testing.T) {
	dir := t.TempDir()
	withMode := filepath.Join(dir, "with-mode.yaml")
	writeFile(t, withMode, "server:\n  mode: \"prod\"\n"+strings.TrimPrefix(filehttpYAML, "server:\n"))

	if _, err := conf.Open[filehttpConfig](context.Background(), file.New(withMode)); err == nil {
		t.Fatal("server.mode present: want unknown-key error, got nil")
	}

	cleaned := filepath.Join(dir, "cleaned.yaml")
	writeFile(t, cleaned, filehttpYAML)
	c, err := conf.Open[filehttpConfig](context.Background(), file.New(cleaned))
	if err != nil {
		t.Fatalf("server.mode removed: open: %v", err)
	}
	if got := c.Current().Server.ListenAddr; got != ":8080" {
		t.Fatalf("listen_addr = %q, want :8080", got)
	}
}

// C08: when both legacy and new names are set, the new name wins; when only
// one is set, that one applies.
// C08：新旧名都设置时新名胜出；只设置一个时该名生效。
func TestC08BucketEnvPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		legacy string
		modern string
		want   string
	}{
		{"both set, new wins", "legacy-bucket", "new-bucket", "new-bucket"},
		{"only legacy", "legacy-bucket", "", "legacy-bucket"},
		{"only new", "", "new-bucket", "new-bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Empty values are absent by spec, so they fall through to the file.
			// 空串按规格视为缺席，回退到文件值。
			t.Setenv("S3_BUCKET", tc.legacy)
			t.Setenv("STORAGE_S3_BUCKET", tc.modern)

			dir := t.TempDir()
			p := filepath.Join(dir, "config.yaml")
			writeFile(t, p, filehttpYAML)

			sources := append([]conf.Source{file.New(p)}, filehttpEnv()...)
			c, err := conf.Open[filehttpConfig](context.Background(), sources...)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if got := c.Current().Storage.S3Bucket; got != tc.want {
				t.Fatalf("s3_bucket = %q, want %q", got, tc.want)
			}
		})
	}
}

// C08: a binding error for the database DSN must not reveal the DSN.
// C08：数据库 DSN 的绑定错误不得泄露 DSN。
func TestC08DSNNotLeaked(t *testing.T) {
	const dsn = "postgres://user:TESTPASS-7c1@host/db"
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	writeFile(t, p, filehttpYAML)
	// A list is not a string, so binding fails. The bad value comes from a
	// higher layer than the file; the ENV layer is a string and would override
	// a file value, so the list is supplied by a final layer that is a map.
	// 列表不是字符串，绑定会失败。错误值来自高于文件的最后一层；ENV 层是字符串，
	// 会覆盖文件值，因此由最后一层的对象提供列表。
	bad := map[string]any{"database": map[string]any{"dsn": []any{dsn}}}
	sources := append([]conf.Source{file.New(p)}, filehttpEnv()...)
	sources = append(sources, &fakeSource{data: bad})
	_, err := conf.Open[filehttpConfig](context.Background(), sources...)
	if err == nil {
		t.Fatal("want bind error")
	}
	if strings.Contains(err.Error(), "TESTPASS") || strings.Contains(err.Error(), dsn) {
		t.Fatalf("error leaks DSN: %q", err.Error())
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
