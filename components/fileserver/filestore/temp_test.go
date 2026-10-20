package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
)

// TestCleanupTempRemovesOnlyOldFiles checks the garbage collector for upload
// temp files. It deliberately writes the file directly: the mechanism under test
// is the age/regular-file filter, not the upload path.
func TestCleanupTempRemovesOnlyOldFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, WithDurability(SyncNone))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	old := filepath.Join(dir, tempDirName, "up-old")
	fresh := filepath.Join(dir, tempDirName, "up-fresh")
	for _, name := range []string{old, fresh} {
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	removed, err := s.CleanupTemp(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupTemp: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old temp file survived: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh temp file was removed: %v", err)
	}
}

func TestCleanupTempRejectsNonPositiveAge(t *testing.T) {
	s := newStore(t)
	for _, d := range []time.Duration{0, -time.Hour} {
		if _, err := s.CleanupTemp(context.Background(), d); !errors.Is(err, fileserver.ErrInvalidArgument) {
			t.Fatalf("CleanupTemp(%s) = %v, want ErrInvalidArgument", d, err)
		}
	}
}
