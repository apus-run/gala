// Command configreload is a service that reloads its YAML config when ops edits
// the file in place. The config is re-read through fileutils.Cache, so requests
// that happen between edits do not touch the disk.
// configreload 演示一个在运维原地修改文件时重载 YAML 配置的服务。
// 配置经 fileutils.Cache 重读，编辑间隙到来的请求不会触碰磁盘。
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/apus-run/gala/pkg/fileutils"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dir, err := os.MkdirTemp("", "configreload")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	// In production the config ships with the binary in /etc/gala or the
	// working directory; Search finds it without hard-coding the install path.
	// 生产环境中配置随二进制放在 /etc/gala 或工作目录；
	// Search 无需硬编码安装路径即可找到它。
	cfgPath := filepath.Join(dir, "gateway.yaml")
	if err := fileutils.WriteFile(cfgPath, "rate_limit: 100\nupstream: http://10.0.0.5:8080\n"); err != nil {
		return err
	}
	path, err := fileutils.Search("gateway.yaml", dir)
	if err != nil {
		return err
	}

	var cfg fileutils.Cache

	// Serve some requests with the current config.
	// 用当前配置服务若干请求。
	for i := range 3 {
		b, err := cfg.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Printf("request %d uses: %s", i+1, firstLine(b))
	}

	// Ops raises the rate limit. The size changes, so the next read sees it.
	// 运维调高限流值。文件大小变化，下一次读取即可看到。
	if err := fileutils.ReplaceFile(path, func(_, s string) string {
		return strings.Replace(s, "rate_limit: 100", "rate_limit: 500", 1)
	}); err != nil {
		return err
	}
	b, err := cfg.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Printf("after reload: %s", firstLine(b))
	return nil
}

// firstLine returns the first line of b, keeping its newline.
// firstLine 返回 b 的第一行，保留行尾换行符。
func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	return string(line) + "\n"
}
