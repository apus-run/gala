//go:build unix

package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

const (
	crashEnvMode = "FILESTORE_CRASH_MODE"
	crashEnvDir  = "FILESTORE_CRASH_DIR"
	crashPart    = 5 << 20
	crashTotal   = 2*crashPart + 1234
)

func crashData() []byte {
	data := make([]byte, crashTotal)
	for i := range data {
		data[i] = byte(i*7 + 3)
	}
	return data
}

// TestCrashHelper 只在子进程中由 runCrash 启动。它执行一次操作，并在指定位置被 SIGKILL 终止。
func TestCrashHelper(t *testing.T) {
	mode := os.Getenv(crashEnvMode)
	if mode == "" {
		t.Skip("helper process; started by the crash tests")
	}
	s, err := New(os.Getenv(crashEnvDir))
	if err != nil {
		t.Fatalf("helper New: %v", err)
	}
	ctx := context.Background()
	data := crashData()
	switch mode {
	case "upload-midstream":
		_ = s.Upload(ctx, "obj", &crashReader{r: bytes.NewReader(data), at: 1 << 20})
	case "publish-then-die":
		die := fileserver.ProgressFunc(func(context.Context, fileserver.ProgressInfo) { killSelf() })
		_ = s.Upload(ctx, "obj", bytes.NewReader(data), fileserver.WithProgress(die))
	default:
		t.Fatalf("unknown crash mode %q", mode)
	}
	t.Fatal("helper survived its crash point")
}

// crashSlice 返回第 n 片（从 1 开始）的内容。
func crashSlice(data []byte, n int) []byte {
	off := (n - 1) * crashPart
	end := min(off+crashPart, len(data))
	return data[off:end]
}

// crashReader 读取累计达到 at 字节时终止进程。
type crashReader struct {
	r    io.Reader
	at   int64
	read int64
}

func (c *crashReader) Read(p []byte) (int, error) {
	if c.read >= c.at {
		killSelf()
	}
	if rem := c.at - c.read; int64(len(p)) > rem {
		p = p[:rem]
	}
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

// killSelf 发送 SIGKILL：无法被捕获，不运行任何 defer，模拟断电或进程被杀。
func killSelf() {
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	time.Sleep(10 * time.Second)
}

// runCrash 在子进程中执行 mode，并断言它被 SIGKILL 终止。
func runCrash(t *testing.T, mode, dir string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), crashEnvMode+"="+mode, crashEnvDir+"="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("helper %s did not terminate abnormally: err=%v output=%s", mode, err, out)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("helper %s exit = %v, want SIGKILL; output: %s", mode, exitErr, out)
	}
}

func TestCrashDuringUploadKeepsPreviousVersion(t *testing.T) {
	dir := t.TempDir()
	prev, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := prev.Upload(context.Background(), "obj", bytes.NewReader([]byte("previous"))); err != nil {
		t.Fatal(err)
	}
	requireNoErrT(t, prev.Close())

	runCrash(t, "upload-midstream", dir)

	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	rc, err := s.Read(ctx, "obj")
	if err != nil {
		t.Fatalf("previous version lost after crash: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != "previous" {
		t.Fatalf("object after crash = %q, want previous version", got)
	}

	leftovers := tempEntries(t, s)
	if len(leftovers) == 0 {
		t.Fatal("crash left no partial temporary file; the scenario did not exercise a crash")
	}
	for _, name := range leftovers {
		backdate(t, filepath.Join(dir, tempDirName, name))
	}
	if removed, err := s.CleanupTemp(ctx, 24*time.Hour); err != nil || removed != len(leftovers) {
		t.Fatalf("CleanupTemp = %d, %v; want %d", removed, err, len(leftovers))
	}
	if err := s.Upload(ctx, "obj", bytes.NewReader(crashData())); err != nil {
		t.Fatalf("retry after crash: %v", err)
	}
}

func TestCrashAfterPublishKeepsCompleteObject(t *testing.T) {
	dir := t.TempDir()
	runCrash(t, "publish-then-die", dir)

	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var buf fileservertest.BufferAt
	if err := s.Download(context.Background(), "obj", &buf); err != nil {
		t.Fatalf("object missing after crash that followed publish: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), crashData()) {
		t.Fatalf("object after crash has %d bytes, want %d complete bytes", len(buf.Bytes()), len(crashData()))
	}
}

// backdate 把路径的修改时间设为两天前，使 CleanupTemp 认为它已经过期。
func backdate(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}
