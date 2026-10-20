package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

// TestSymlinkEscapeIsPermissionDenied pins the classification of os.Root sandbox
// errors. os.Root blocks the access (which is what matters), but without this
// mapping the caller gets an unclassified error it cannot branch on.
func TestSymlinkEscapeIsPermissionDenied(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, dataDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, dataDirName, "escape")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	s, err := New(root, WithDurability(SyncNone))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := s.Stat(ctx, "escape"); !errors.Is(err, fileserver.ErrPermission) {
		t.Fatalf("Stat = %v, want ErrPermission", err)
	}
	if _, err := s.Read(ctx, "escape"); !errors.Is(err, fileserver.ErrPermission) {
		t.Fatalf("Read = %v, want ErrPermission", err)
	}
	if err := s.Download(ctx, "escape", &discardAt{}); !errors.Is(err, fileserver.ErrPermission) {
		t.Fatalf("Download = %v, want ErrPermission", err)
	}
	if err := s.Upload(ctx, "escape", nil); !errors.Is(err, fileserver.ErrInvalidArgument) {
		t.Fatalf("Upload with a nil reader = %v, want ErrInvalidArgument", err)
	}
}

type discardAt struct{}

func (*discardAt) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }
