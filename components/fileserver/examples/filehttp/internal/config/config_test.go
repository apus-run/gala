package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testConfig = `
server:
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
`

func writeConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("CONFIG_PATH", writeConfig(t))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenAddr != ":8080" ||
		cfg.Server.MaxUploadBytes != 524288000 ||
		cfg.Database.DSN != "./data/filehost.db" ||
		cfg.Storage.FSRoot != "./data" ||
		cfg.Storage.S3Region != "us-east-1" ||
		!cfg.Storage.S3PathStyle {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLegacyEnvOverrides(t *testing.T) {
	t.Setenv("CONFIG_PATH", writeConfig(t))
	t.Setenv("LISTEN_ADDR", ":9999")
	t.Setenv("MAX_UPLOAD_BYTES", "1234")
	t.Setenv("DB_DSN", "/tmp/filehttp.db")
	t.Setenv("FS_ROOT", "/tmp/filehttp")
	t.Setenv("S3_BUCKET", "bucket-x")
	t.Setenv("S3_ENDPOINT", "http://rustfs:9000")
	t.Setenv("S3_PREFIX", "tenant")
	t.Setenv("S3_PATH_STYLE", "false")
	t.Setenv("S3_REGION", "eu-west-1")
	t.Setenv("S3_CREATE_BUCKET", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenAddr != ":9999" ||
		cfg.Server.MaxUploadBytes != 1234 ||
		cfg.Database.DSN != "/tmp/filehttp.db" ||
		cfg.Storage.FSRoot != "/tmp/filehttp" ||
		cfg.Storage.S3Bucket != "bucket-x" ||
		cfg.Storage.S3Endpoint != "http://rustfs:9000" ||
		cfg.Storage.S3Prefix != "tenant" ||
		cfg.Storage.S3Region != "eu-west-1" ||
		!cfg.Storage.S3CreateBucket ||
		cfg.Storage.S3PathStyle {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestNewEnvNameOverrides(t *testing.T) {
	t.Setenv("CONFIG_PATH", writeConfig(t))
	t.Setenv("STORAGE_S3_REGION", "ap-south-1")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage.S3Region != "ap-south-1" {
		t.Fatalf("S3Region = %q", cfg.Storage.S3Region)
	}
}

func TestMissingConfig(t *testing.T) {
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yaml"))
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a missing config")
	}
}
