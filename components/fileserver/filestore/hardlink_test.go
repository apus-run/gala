package filestore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// hardlinkStub 包装真实的 os.Root，只替换 Link 的返回值，用来模拟文件系统行为。
type hardlinkStub struct {
	*os.Root
	linkErr error
	linkN   int
}

func (h *hardlinkStub) Link(oldname, newname string) error {
	h.linkN++
	if h.linkErr != nil {
		return h.linkErr
	}
	return h.Root.Link(oldname, newname)
}

// newProbeRoot 建立一个含临时区的根目录，并返回它的句柄。
func newProbeRoot(t *testing.T) *os.Root {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, tempDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// probeEntries 返回临时区中的文件名。
func probeEntries(t *testing.T, r *os.Root) []string {
	t.Helper()
	f, err := r.Open(tempDirName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestProbeHardlinkClassifiesFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want linkMode
	}{
		{"not supported", syscall.ENOTSUP, linkUnsupported},
		{"not supported variant", syscall.EOPNOTSUPP, linkUnsupported},
		{"unsupported sentinel", errors.ErrUnsupported, linkUnsupported},
		{"denied", syscall.EPERM, linkDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newProbeRoot(t)
			stub := &hardlinkStub{Root: root, linkErr: tc.err}
			got, err := probeHardlink(stub)
			if err != nil {
				t.Fatalf("probeHardlink error = %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("probeHardlink = %v, want %v", got, tc.want)
			}
			if names := probeEntries(t, root); len(names) != 0 {
				t.Fatalf("probe left files behind: %v", names)
			}
		})
	}
}

func TestProbeHardlinkFailsOnUnexpectedError(t *testing.T) {
	root := newProbeRoot(t)
	boom := errors.New("boom")
	stub := &hardlinkStub{Root: root, linkErr: boom}
	got, err := probeHardlink(stub)
	if !errors.Is(err, boom) {
		t.Fatalf("probeHardlink error = %v, want boom", err)
	}
	if got != linkOK {
		t.Fatalf("probeHardlink = %v, want linkOK", got)
	}
	if names := probeEntries(t, root); len(names) != 0 {
		t.Fatalf("probe left files behind: %v", names)
	}
}

func TestProbeHardlinkGivesUpAfterRepeatedConflicts(t *testing.T) {
	root := newProbeRoot(t)
	stub := &hardlinkStub{Root: root, linkErr: fs.ErrExist}
	if _, err := probeHardlink(stub); err == nil {
		t.Fatal("probeHardlink accepted a filesystem that always reports the link as existing")
	}
	if stub.linkN != maxTempAttempts {
		t.Fatalf("link attempts = %d, want %d", stub.linkN, maxTempAttempts)
	}
	if names := probeEntries(t, root); len(names) != 0 {
		t.Fatalf("probe left files behind: %v", names)
	}
}

func TestIsLinkUnsupported(t *testing.T) {
	if !isLinkUnsupported(syscall.ENOTSUP) || !isLinkUnsupported(syscall.EOPNOTSUPP) || !isLinkUnsupported(errors.ErrUnsupported) {
		t.Fatal("a hard-link-unsupported error was not recognized")
	}
	if isLinkUnsupported(syscall.EPERM) || isLinkUnsupported(errors.New("boom")) {
		t.Fatal("an unrelated error was reported as unsupported")
	}
}

func TestIsLinkDenied(t *testing.T) {
	if !isLinkDenied(syscall.EPERM) {
		t.Fatal("EPERM was not recognized as denied")
	}
	if isLinkDenied(syscall.ENOTSUP) || isLinkDenied(errors.New("boom")) {
		t.Fatal("an unrelated error was reported as denied")
	}
}
