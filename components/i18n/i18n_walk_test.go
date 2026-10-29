package i18n

import (
	"os"
	"path/filepath"
	"testing"
)

// 目录里有不可读的子目录时，遍历要在该处停止，其后的文件不再加载。
// 原实现忽略了 Walk 传来的错误，会继续遍历并把其余文件都加载进来。
func TestAddDirStopsAtUnreadableEntry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "a_locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "en.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	// 字典序在 a_locked 之后：遍历在不可读目录处停止时，它不会被加载
	if err := os.WriteFile(filepath.Join(dir, "z.en.json"), []byte(`{"hello":"world"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	i := New()
	i.Add(dir)
	if got := i.T("hello"); got == "world" {
		t.Fatal("Add kept walking past an unreadable directory entry and loaded later files")
	}
}
