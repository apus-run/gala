package fileutils

import (
	"os"
	"path/filepath"
	"testing"
)

// put writes content to dir/name (creating parents) and returns the full path.
func put(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := WriteFile(p, content); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPredicates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := put(t, dir, "f.txt", "x")
	empty := put(t, dir, "empty.txt", "")
	emptyDir := filepath.Join(dir, "ed")
	if err := os.Mkdir(emptyDir, 0o750); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nope")

	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"Exists file", Exists(file), true},
		{"Exists dir", Exists(dir), true},
		{"Exists missing", Exists(missing), false},
		{"IsDir dir", IsDir(dir), true},
		{"IsDir file", IsDir(file), false},
		{"IsDir missing", IsDir(missing), false},
		{"IsFile file", IsFile(file), true},
		{"IsFile dir", IsFile(dir), false},
		{"IsFile missing", IsFile(missing), false},
		{"IsEmpty non-empty file", IsEmpty(file), false},
		{"IsEmpty zero file", IsEmpty(empty), true},
		{"IsEmpty empty dir", IsEmpty(emptyDir), true},
		{"IsEmpty non-empty dir", IsEmpty(dir), false},
		{"IsEmpty missing", IsEmpty(missing), true},
		{"IsReadable file", IsReadable(file), true},
		{"IsReadable missing", IsReadable(missing), false},
		{"IsWritable file", IsWritable(file), true},
		{"IsWritable dir", IsWritable(dir), true},
		{"IsWritable missing", IsWritable(missing), false},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if got := read(t, file); got != "x" {
		t.Errorf("IsWritable must not truncate the file, content = %q", got)
	}
	if left, _ := os.ReadDir(emptyDir); len(left) != 0 {
		t.Errorf("IsWritable left %d entries behind", len(left))
	}
}

func TestRealPath(t *testing.T) { //nolint:paralleltest // t.Chdir cannot be used in a parallel test
	dir := t.TempDir()
	file := put(t, dir, "f.txt", "x")
	t.Chdir(dir)

	if got := RealPath("f.txt"); got != filepath.Join(mustEval(t, dir), "f.txt") && got != file {
		t.Errorf("RealPath(relative) = %q", got)
	}
	if got := RealPath("missing"); got != "" {
		t.Errorf("RealPath(missing) = %q, want empty", got)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNameAndExt(t *testing.T) {
	t.Parallel()
	cases := []struct{ path, name, ext string }{
		{"/var/www/file.js", "file", "js"},
		{"file.js", "file", "js"},
		{"archive.tar.gz", "archive.tar", "gz"},
		{"noext", "noext", ""},
		{"/a/b/", "b", ""},
	}
	for _, c := range cases {
		if got := Name(c.path); got != c.name {
			t.Errorf("Name(%q) = %q, want %q", c.path, got, c.name)
		}
		if got := ExtName(c.path); got != c.ext {
			t.Errorf("ExtName(%q) = %q, want %q", c.path, got, c.ext)
		}
	}
}
