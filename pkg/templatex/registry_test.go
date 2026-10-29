package templatex

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func checkRegistryFuncs(t *testing.T, r *Registry, expectedFuncs ...string) {
	if v := len(r.funcs); v != len(expectedFuncs) {
		t.Fatalf("Expected total %d funcs, got %d", len(expectedFuncs), v)
	}

	for _, name := range expectedFuncs {
		if _, ok := r.funcs[name]; !ok {
			t.Fatalf("Missing %q func", name)
		}
	}
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()

	if r.cache == nil {
		t.Fatalf("Expected cache store to be initialized, got nil")
	}

	if v := r.cache.Len(); v != 0 {
		t.Fatalf("Expected cache store length to be 0, got %d", v)
	}

	checkRegistryFuncs(t, r, "raw")
}

func TestRegistryAddFuncs(t *testing.T) {
	r := NewRegistry()

	r.AddFuncs(map[string]any{
		"test": func(a string) string { return a + "-TEST" },
	})

	checkRegistryFuncs(t, r, "raw", "test")

	result, err := r.LoadString(`{{.|test}}`).Render("example")
	if err != nil {
		t.Fatalf("Unexpected Render() error, got %v", err)
	}

	expected := "example-TEST"
	if result != expected {
		t.Fatalf("Expected Render() result %q, got %q", expected, result)
	}
}

func TestRegistryLoadFiles(t *testing.T) {
	r := NewRegistry()

	t.Run("invalid or missing files", func(t *testing.T) {
		r.LoadFiles("file1.missing", "file2.missing")

		renderer, _ := r.cache.Get(filesKey([]string{"file1.missing", "file2.missing"}))

		if renderer == nil {
			t.Fatal("Expected renderer to be initialized even if invalid, got nil")
		}

		if renderer.template != nil {
			t.Fatalf("Expected renderer templatex to be nil, got %v", renderer.template)
		}

		if renderer.parseError == nil {
			t.Fatalf("Expected renderer parseError to be set, got nil")
		}
	})

	t.Run("valid files", func(t *testing.T) {
		// create test templates
		dir, err := os.MkdirTemp(os.TempDir(), "template_test")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "base.html"), []byte(`Base:{{template "content" .}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "content.html"), []byte(`{{define "content"}}Content:{{.|raw}}{{end}}`), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)

		files := []string{filepath.Join(dir, "base.html"), filepath.Join(dir, "content.html")}

		r.LoadFiles(files...)

		renderer, _ := r.cache.Get(filesKey(files))

		if renderer == nil {
			t.Fatal("Expected renderer to be initialized even if invalid, got nil")
		}

		if renderer.template == nil {
			t.Fatal("Expected renderer templatex to be set, got nil")
		}

		if renderer.parseError != nil {
			t.Fatalf("Expected renderer parseError to be nil, got %v", renderer.parseError)
		}

		result, err := renderer.Render("<h1>123</h1>")
		if err != nil {
			t.Fatalf("Unexpected Render() error, got %v", err)
		}

		expected := "Base:Content:<h1>123</h1>"
		if result != expected {
			t.Fatalf("Expected Render() result %q, got %q", expected, result)
		}
	})
}

func TestRegistryLoadString(t *testing.T) {
	r := NewRegistry()

	t.Run("invalid templatex string", func(t *testing.T) {
		txt := `test {{define "content"}}`

		r.LoadString(txt)

		renderer, _ := r.cache.Get(stringKey(txt))

		if renderer == nil {
			t.Fatal("Expected renderer to be initialized even if invalid, got nil")
		}

		if renderer.template != nil {
			t.Fatalf("Expected renderer templatex to be nil, got %v", renderer.template)
		}

		if renderer.parseError == nil {
			t.Fatalf("Expected renderer parseError to be set, got nil")
		}
	})

	t.Run("valid templatex string", func(t *testing.T) {
		txt := `test {{.|raw}}`

		r.LoadString(txt)

		renderer, _ := r.cache.Get(stringKey(txt))

		if renderer == nil {
			t.Fatal("Expected renderer to be initialized even if invalid, got nil")
		}

		if renderer.template == nil {
			t.Fatal("Expected renderer templatex to be set, got nil")
		}

		if renderer.parseError != nil {
			t.Fatalf("Expected renderer parseError to be nil, got %v", renderer.parseError)
		}

		result, err := renderer.Render("<h1>123</h1>")
		if err != nil {
			t.Fatalf("Unexpected Render() error, got %v", err)
		}

		expected := "test <h1>123</h1>"
		if result != expected {
			t.Fatalf("Expected Render() result %q, got %q", expected, result)
		}
	})
}

// Regression: %v formatted ["x y"] and ["x", "y"] to the same cache key,
// silently returning the wrong template for one of the two sets.
func TestLoadFSKeyNoCollision(t *testing.T) {
	fsys := fstest.MapFS{
		"x y": &fstest.MapFile{Data: []byte(`single:{{.}}`)},
		"x":   &fstest.MapFile{Data: []byte(`x:{{.}}`)},
		"y":   &fstest.MapFile{Data: []byte(`y:{{.}}`)},
	}

	r := NewRegistry()

	single, err := r.LoadFS(fsys, "x y").Render("v")
	if err != nil {
		t.Fatal(err)
	}

	multi, err := r.LoadFS(fsys, "x", "y").Render("v")
	if err != nil {
		t.Fatal(err)
	}

	if single != "single:v" || multi != "x:v" {
		t.Fatalf("Expected single:v and x:v, got %q and %q", single, multi)
	}
}

// Regression: LoadFiles() with no filenames panicked with index out of range.
func TestLoadFilesEmpty(t *testing.T) {
	if _, err := NewRegistry().LoadFiles().Render(nil); err == nil {
		t.Fatal("Expected error for missing filenames, got nil")
	}
}

func TestRegistryReset(t *testing.T) {
	r := NewRegistry()

	if _, err := r.LoadString(`ok:{{.}}`).Render("v"); err != nil {
		t.Fatal(err)
	}
	if v := r.cache.Len(); v != 1 {
		t.Fatalf("Expected 1 cached entry, got %d", v)
	}

	r.Reset()

	if v := r.cache.Len(); v != 0 {
		t.Fatalf("Expected empty cache after Reset, got %d", v)
	}

	if result, err := r.LoadString(`ok:{{.}}`).Render("v"); err != nil || result != "ok:v" {
		t.Fatalf("Expected ok:v after Reset, got %q, %v", result, err)
	}
}

func TestRegistryLoadFS(t *testing.T) {
	r := NewRegistry()

	t.Run("invalid fs", func(t *testing.T) {
		fs := os.DirFS("__missing__")

		files := []string{"missing1", "missing2"}

		key := fsKey(fs, files)

		r.LoadFS(fs, files...)

		renderer, _ := r.cache.Get(key)

		if renderer == nil {
			t.Fatal("Expected renderer to be initialized even if invalid, got nil")
		}

		if renderer.template != nil {
			t.Fatalf("Expected renderer templatex to be nil, got %v", renderer.template)
		}

		if renderer.parseError == nil {
			t.Fatalf("Expected renderer parseError to be set, got nil")
		}
	})

	t.Run("valid fs", func(t *testing.T) {
		// create test templates
		dir, err := os.MkdirTemp(os.TempDir(), "template_test2")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "base.html"), []byte(`Base:{{template "content" .}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "content.html"), []byte(`{{define "content"}}Content:{{.|raw}}{{end}}`), 0644); err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)

		fs := os.DirFS(dir)

		files := []string{"base.html", "content.html"}

		key := fsKey(fs, files)

		r.LoadFS(fs, files...)

		renderer, _ := r.cache.Get(key)

		if renderer == nil {
			t.Fatal("Expected renderer to be initialized even if invalid, got nil")
		}

		if renderer.template == nil {
			t.Fatal("Expected renderer template to be set, got nil")
		}

		if renderer.parseError != nil {
			t.Fatalf("Expected renderer parseError to be nil, got %v", renderer.parseError)
		}

		result, err := renderer.Render("<h1>123</h1>")
		if err != nil {
			t.Fatalf("Unexpected Render() error, got %v", err)
		}

		expected := "Base:Content:<h1>123</h1>"
		if result != expected {
			t.Fatalf("Expected Render() result %q, got %q", expected, result)
		}
	})
}

// LoadString 的模板文本恰好等于一个已加载的文件路径时，不能命中文件模板的缓存。
func TestLoadStringDoesNotCollideWithFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.tpl")
	if err := os.WriteFile(path, []byte("from-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	if got, err := r.LoadFiles(path).Render(nil); err != nil || got != "from-file" {
		t.Fatalf("LoadFiles = %q, %v", got, err)
	}
	got, err := r.LoadString(path).Render(nil)
	if err != nil || got != path {
		t.Fatalf("LoadString(path) = %q, %v; want the string itself, not the cached file template", got, err)
	}
}

// "a,b"（一个文件名）和 "a","b"（两个文件）必须得到不同的缓存 key：
// 用逗号拼接文件名时，这两种加载会取到同一个缓存条目。
func TestLoadFilesKeyKeepsFilenameBoundaries(t *testing.T) {
	if filesKey([]string{"a,b"}) == filesKey([]string{"a", "b"}) {
		t.Fatal("one file named 'a,b' and the two files 'a','b' map to the same cache key")
	}
}
