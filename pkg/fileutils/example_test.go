package fileutils_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/apus-run/gala/pkg/fileutils"
)

func ExampleWriteFile() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	// Missing parent directories are created.
	path := filepath.Join(dir, "conf", "a.yaml")
	if err := fileutils.WriteFile(path, "name: demo"); err != nil {
		panic(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))

	// Output:
	// name: demo
}

func ExampleAppendFile() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "app.log")
	if err := fileutils.WriteFile(path, "started\n"); err != nil {
		panic(err)
	}
	if err := fileutils.AppendFile(path, []byte("ready\n")); err != nil {
		panic(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(data))

	// Output:
	// started
	// ready
}

func ExampleLines() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	// Mixed "\n" and "\r\n" terminators; the last line has none.
	path := filepath.Join(dir, "words.txt")
	if err := os.WriteFile(path, []byte("alpha\r\nbeta\ngamma"), 0o644); err != nil {
		panic(err)
	}

	for line, err := range fileutils.Lines(path) {
		if err != nil {
			panic(err)
		}
		fmt.Printf("%q\n", line)
	}

	// Output:
	// "alpha"
	// "beta"
	// "gamma"
}

func ExampleScan() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	for _, p := range []string{"a.go", "b.txt", "sub/c.go", "sub/deep/d.go"} {
		if err := fileutils.WriteFile(filepath.Join(dir, p), "package x"); err != nil {
			panic(err)
		}
	}

	// Recursive search for Go files only, directories excluded.
	paths, err := fileutils.Scan(dir, fileutils.ScanOptions{
		Patterns:  []string{"*.go"},
		Recursive: true,
		FilesOnly: true,
	})
	if err != nil {
		panic(err)
	}
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			panic(err)
		}
		fmt.Println(filepath.ToSlash(rel))
	}

	// Output:
	// a.go
	// sub/c.go
	// sub/deep/d.go
}

func ExampleScan_filter() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	for _, p := range []string{"a.go", "sub/c.go", "sub/deep/d.go"} {
		if err := fileutils.WriteFile(filepath.Join(dir, p), "package x"); err != nil {
			panic(err)
		}
	}

	// Filter runs after Patterns; here it drops everything under a "deep" directory.
	paths, err := fileutils.Scan(dir, fileutils.ScanOptions{
		Patterns:  []string{"*.go"},
		Recursive: true,
		FilesOnly: true,
		Filter: func(p string, _ fs.DirEntry) bool {
			ok, _ := fileutils.MatchGlob("**/deep/**", filepath.ToSlash(p))
			return !ok
		},
	})
	if err != nil {
		panic(err)
	}
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			panic(err)
		}
		fmt.Println(filepath.ToSlash(rel))
	}

	// Output:
	// a.go
	// sub/c.go
}

func ExampleMatchGlob() {
	// "**" matches any number of path segments, including none.
	fmt.Println(fileutils.MatchGlob("**/internal/**", "/src/internal/x/a.go"))
	fmt.Println(fileutils.MatchGlob("**/internal/**", "/src/api/a.go"))

	// Output:
	// true <nil>
	// false <nil>
}

func ExampleReplaceFile() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "version.txt")
	if err := fileutils.WriteFile(path, "release v1"); err != nil {
		panic(err)
	}
	err = fileutils.ReplaceFile(path, func(_, s string) string {
		return fmt.Sprintf("%s (upgraded)", s)
	})
	if err != nil {
		panic(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))

	// Output:
	// release v1 (upgraded)
}

func ExampleReplaceDir() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	for p, content := range map[string]string{
		"a.md":     "intro v1",
		"sub/b.md": "guide v1",
		"c.txt":    "v1 stays",
	} {
		if err := fileutils.WriteFile(filepath.Join(dir, p), content); err != nil {
			panic(err)
		}
	}

	// Only *.md files are passed to the callback; c.txt is never touched.
	err = fileutils.ReplaceDir(dir, func(_, s string) string {
		return strings.ReplaceAll(s, "v1", "v2")
	}, fileutils.ScanOptions{Patterns: []string{"*.md"}, Recursive: true})
	if err != nil {
		panic(err)
	}

	for _, p := range []string{"a.md", "sub/b.md", "c.txt"} {
		data, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s: %s\n", filepath.ToSlash(p), data)
	}

	// Output:
	// a.md: intro v2
	// sub/b.md: guide v2
	// c.txt: v1 stays
}

func ExampleCopy() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	src := filepath.Join(dir, "tpl")
	if err := fileutils.WriteFile(filepath.Join(src, "sub", "index.html"), "<h1>hi</h1>"); err != nil {
		panic(err)
	}

	// Copies the whole tree; Sync flushes each file to disk.
	dst := filepath.Join(dir, "out")
	if err := fileutils.Copy(src, dst, fileutils.CopyOption{Sync: true}); err != nil {
		panic(err)
	}

	data, err := os.ReadFile(filepath.Join(dst, "sub", "index.html"))
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))

	// Output:
	// <h1>hi</h1>
}

func ExampleCache_ReadFile() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "index.html")
	if err := fileutils.WriteFile(path, "v1"); err != nil {
		panic(err)
	}

	var cache fileutils.Cache

	// Unchanged file: the second read is served from memory.
	for range 2 {
		data, err := cache.ReadFile(path)
		if err != nil {
			panic(err)
		}
		fmt.Println(string(data))
	}

	// A change in size is noticed on the next read.
	if err := fileutils.WriteFile(path, "version two"); err != nil {
		panic(err)
	}
	data, err := cache.ReadFile(path)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))

	// Output:
	// v1
	// v1
	// version two
}

func ExampleSearch() {
	dir, err := os.MkdirTemp("", "fileutils")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	if err := fileutils.WriteFile(filepath.Join(dir, "app.yaml"), "name: demo"); err != nil {
		panic(err)
	}

	// Candidate directories are checked in order; the first hit wins.
	path, err := fileutils.Search("app.yaml", filepath.Join(dir, "missing"), dir)
	if err != nil {
		panic(err)
	}
	fmt.Println(filepath.Base(path))

	// Output:
	// app.yaml
}

func ExampleFormatSize() {
	fmt.Println(fileutils.FormatSize(1536))
	fmt.Println(fileutils.FormatSize(64 << 20))

	// Output:
	// 1.50K
	// 64.00M
}

func ExampleParseSize() {
	n, err := fileutils.ParseSize("64 MB")
	if err != nil {
		panic(err)
	}
	fmt.Println(n)

	// Output:
	// 67108864
}
