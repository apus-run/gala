// Command uploadquota is the storage side of a file-sharing service. Each tenant
// has a directory and a byte quota. Uploads are rejected when they would exceed
// the quota, and abandoned chunk files (*.part) are swept after they expire.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/apus-run/gala/pkg/fileutils"
)

const quota = 1 << 20 // 1 MiB per tenant, for the example

var errQuota = errors.New("quota exceeded")

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	root, err := os.MkdirTemp("", "uploadquota")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)

	tenant := filepath.Join(root, "tenant-acme")

	// Accepted: well within the quota. WriteFile creates the tenant directory.
	if err := store(tenant, "report-q1.pdf", 600<<10); err != nil {
		return err
	}
	// Rejected: a second 600 KiB file would push the tenant past 1 MiB.
	if err := store(tenant, "report-q2.pdf", 600<<10); err != nil {
		fmt.Println("rejected report-q2.pdf:", err)
	}

	// An interrupted chunked upload left a stale part file behind. Make it old.
	stale := filepath.Join(tenant, "upload-9f2c.part")
	if err := fileutils.WriteFile(stale, "partial"); err != nil {
		return err
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		return err
	}
	removed, err := sweepParts(tenant, 24*time.Hour)
	if err != nil {
		return err
	}
	fmt.Println("swept part files:", removed)

	used, err := usage(tenant)
	if err != nil {
		return err
	}
	fmt.Printf("tenant usage: %s of %s\n", fileutils.FormatSize(used), fileutils.FormatSize(quota))
	return nil
}

// store writes a file for the tenant after checking the quota.
func store(tenant, name string, size int) error {
	used, err := usage(tenant)
	if err != nil {
		return err
	}
	if used+int64(size) > quota {
		return fmt.Errorf("%s (%s): %w", name, fileutils.FormatSize(int64(size)), errQuota)
	}
	body := make([]byte, size)
	return fileutils.WriteFile(filepath.Join(tenant, name), body)
}

// usage sums the sizes of the tenant's files. A tenant without a directory has
// no usage yet.
func usage(tenant string) (int64, error) {
	files, err := fileutils.Scan(tenant, fileutils.ScanOptions{Recursive: true, FilesOnly: true})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var total int64
	for _, p := range files {
		fi, err := os.Stat(p)
		if err != nil {
			return 0, err
		}
		total += fi.Size()
	}
	return total, nil
}

// sweepParts removes *.part files older than maxAge and reports how many it removed.
func sweepParts(tenant string, maxAge time.Duration) (int, error) {
	parts, err := fileutils.Scan(tenant, fileutils.ScanOptions{Patterns: []string{"*.part"}, FilesOnly: true})
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, p := range parts {
		fi, err := os.Stat(p)
		if err != nil {
			return removed, err
		}
		if time.Since(fi.ModTime()) < maxAge {
			continue
		}
		if err := os.Remove(p); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
