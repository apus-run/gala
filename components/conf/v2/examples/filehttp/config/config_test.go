package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apus-run/gala/components/conf/v2/examples/filehttp/config"
)

// testdata is a copy of filehttp's real config.yaml with server.mode removed.
// config.with-mode.yaml is the untouched original.
// testdata 是 filehttp 真实 config.yaml 删除 server.mode 后的副本；
// config.with-mode.yaml 是未改动的原件。
const (
	cleaned  = "../testdata/config.yaml"
	original = "../testdata/config.with-mode.yaml"
)

// clearEnv unsets every name the loader reads, so each test starts from the file.
// clearEnv 取消加载器读取的所有变量，使每个测试从文件值开始。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LISTEN_ADDR", "MAX_UPLOAD_BYTES", "DB_DSN", "FS_ROOT", "S3_BUCKET",
		"S3_REGION", "S3_ENDPOINT", "S3_PREFIX", "S3_PATH_STYLE", "S3_CREATE_BUCKET",
		"SERVER_LISTEN_ADDR", "SERVER_MAX_UPLOAD_BYTES", "DATABASE_DSN",
		"STORAGE_FS_ROOT", "STORAGE_S3_BUCKET", "STORAGE_S3_REGION",
		"STORAGE_S3_ENDPOINT", "STORAGE_S3_PREFIX", "STORAGE_S3_PATH_STYLE",
		"STORAGE_S3_CREATE_BUCKET",
	} {
		t.Setenv(k, "")
	}
}

// The cleaned file loads, and the values match the real file.
// 删除后的文件可以加载，且值与真实文件一致。
func TestCleanedConfigLoads(t *testing.T) {
	clearEnv(t)
	c, err := config.Load(t.Context(), cleaned)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := c.Current()
	if got.Server.ListenAddr != ":8080" || got.Server.MaxUploadBytes != 524288000 {
		t.Fatalf("server = %+v", got.Server)
	}
	if got.Database.DSN != "./data/filehost.db" || got.Storage.FSRoot != "./data" {
		t.Fatalf("db/storage = %+v / %+v", got.Database, got.Storage)
	}
	if got.Storage.S3Region != "us-east-1" || !got.Storage.S3PathStyle {
		t.Fatalf("s3 = %+v", got.Storage)
	}
}

// With server.mode present, strict binding rejects the file (spec C08).
// 存在 server.mode 时，严格绑定拒绝该文件（规格 C08）。
func TestOriginalWithModeIsRejected(t *testing.T) {
	clearEnv(t)
	if _, err := config.Load(t.Context(), original); err == nil {
		t.Fatal("original with server.mode: want error, got nil")
	}
}

// Both legacy and new bucket names set: the new name wins.
// 新旧桶名都设置时，新名胜出。
func TestBucketNewNameWins(t *testing.T) {
	clearEnv(t)
	t.Setenv("S3_BUCKET", "legacy-bucket")
	t.Setenv("STORAGE_S3_BUCKET", "new-bucket")
	c, err := config.Load(t.Context(), cleaned)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := c.Current().Storage.S3Bucket; got != "new-bucket" {
		t.Fatalf("s3_bucket = %q, want new-bucket", got)
	}
}

// Spec §10: the new name set to "" while the legacy name has a value. Empty
// means absent, so the legacy value applies. Matches the old conf (probed).
// 规格第 10 节：新名为空串、旧名有值时，空串视为缺席，旧名生效。与旧版一致（已实测）。
func TestBucketEmptyNewNameFallsBackToLegacy(t *testing.T) {
	clearEnv(t)
	t.Setenv("S3_BUCKET", "legacy-bucket")
	t.Setenv("STORAGE_S3_BUCKET", "")
	c, err := config.Load(t.Context(), cleaned)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := c.Current().Storage.S3Bucket; got != "legacy-bucket" {
		t.Fatalf("s3_bucket = %q, want legacy-bucket", got)
	}
}

// A bind error on a secret-bearing field must not echo the secret. A list-valued
// DSN is a bind error whose raw value is the secret.
// 含密钥字段的绑定错误不得回显密钥。列表形式的 DSN 产生绑定错误，其原始值即密钥。
func TestBindErrorDoesNotEchoSecret(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	const secret = "TESTPASS-7c1"
	body := strings.Replace(mustRead(t, cleaned), `dsn: "./data/filehost.db"`, `dsn: ["`+secret+`"]`, 1)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(t.Context(), p)
	if err == nil {
		t.Fatal("want bind error for list-valued dsn")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks secret: %q", err.Error())
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
