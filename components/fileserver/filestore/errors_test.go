package filestore

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

func TestClassifyMapsOSErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"missing", fs.ErrNotExist, fileserver.ErrNotFound},
		{"permission", fs.ErrPermission, fileserver.ErrPermission},
		{"escape", errors.New("openat .: path escapes from parent"), fileserver.ErrPermission},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.err)
			if !errors.Is(got, tc.want) {
				t.Fatalf("classify(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("classify(%v) dropped the cause", tc.err)
			}
		})
	}
}

func TestClassifyKeepsUnknownErrors(t *testing.T) {
	boom := errors.New("boom")
	if got := classify(boom); !errors.Is(got, boom) {
		t.Fatalf("classify(boom) = %v, want boom", got)
	}
	if got := classify(nil); got != nil {
		t.Fatalf("classify(nil) = %v, want nil", got)
	}
}

func TestIsNotExistCoversNotDir(t *testing.T) {
	if !isNotExist(syscall.ENOTDIR) {
		t.Fatal("ENOTDIR was not treated as a missing path")
	}
	if isNotExist(syscall.EPERM) {
		t.Fatal("EPERM was treated as a missing path")
	}
}

func TestInnerErrRemovesThePath(t *testing.T) {
	err := &fs.PathError{Op: "open", Path: "/secret/place/file", Err: fs.ErrNotExist}
	got := innerErr(err)
	if got != fs.ErrNotExist {
		t.Fatalf("innerErr = %v, want fs.ErrNotExist", got)
	}
	if got := innerErr(os.ErrClosed); got != os.ErrClosed {
		t.Fatalf("innerErr = %v, want os.ErrClosed", got)
	}
}
