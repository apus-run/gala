// Command configreload is a service that reloads its YAML config when ops edits
// the file in place. The config is re-read through fileutils.Cache, so requests
// that happen between edits do not touch the disk.
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
	for i := range 3 {
		b, err := cfg.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Printf("request %d uses: %s", i+1, firstLine(b))
	}

	// Ops raises the rate limit. The size changes, so the next read sees it.
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
func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	return string(line) + "\n"
}
