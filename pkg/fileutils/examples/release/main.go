// Command release packages a static web frontend for a versioned release. It
// copies the build output into dist/<version>, stamps the version into every
// HTML and JS file, and prints the artifact sizes for the release notes.
// release 演示把静态前端打包进带版本号的发布：把构建产物复制到
// dist/<version>，把版本号写进每个 HTML 与 JS 文件，
// 并打印各产物大小供发布说明使用。
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/apus-run/gala/pkg/fileutils"
)

const version = "v1.4.2"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	work, err := os.MkdirTemp("", "release")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	// build/ is what the frontend build produced; in CI it is the checked-out output.
	// build/ 是前端构建的产物；在 CI 中是检出的构建输出。
	build := filepath.Join(work, "build")
	for name, body := range map[string]string{
		"index.html":      "<title>Console</title><script>const v = '__VERSION__';</script>",
		"static/app.js":   "console.log('__VERSION__')",
		"static/app.css":  "body{}",
		"static/logo.svg": "<svg/>",
	} {
		if err := fileutils.WriteFile(filepath.Join(build, name), body); err != nil {
			return err
		}
	}

	// Copy into a versioned directory so old releases stay intact for rollback.
	// 复制到带版本号的目录，旧发布保持原样以便回滚。
	dist := filepath.Join(work, "dist", version)
	if err := fileutils.Copy(build, dist, fileutils.CopyOption{Sync: true}); err != nil {
		return err
	}

	// Stamp the version into text assets only; images are left alone.
	// 只把版本号写进文本资源；图片保持原样。
	err = fileutils.ReplaceDir(dist, func(_, s string) string {
		return strings.ReplaceAll(s, "__VERSION__", version)
	}, fileutils.ScanOptions{Patterns: []string{"*.html", "*.js"}, Recursive: true})
	if err != nil {
		return err
	}

	fmt.Printf("release %s\n", version)
	files, err := fileutils.Scan(dist, fileutils.ScanOptions{Recursive: true, FilesOnly: true})
	if err != nil {
		return err
	}
	for _, p := range files {
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dist, p)
		if err != nil {
			return err
		}
		fmt.Printf("  %-18s %s\n", filepath.ToSlash(rel), fileutils.FormatSize(fi.Size()))
	}
	return nil
}
