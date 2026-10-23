package fileserver_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

func TestUploadSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	// 路径任务构造时文件还不存在，证明没有提前打开。
	fromPath := fileserver.UploadTaskFromPath("path", path, fileserver.WithSize(7))
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := []byte("payload")
	fromBytes := fileserver.UploadTaskFromBytes("bytes", data)
	data[0] = 'X' // 任务必须保留构造时的快照。
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "original.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "payload"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := req.ParseMultipartForm(1); err != nil {
		t.Fatal(err)
	}
	defer req.MultipartForm.RemoveAll()
	fromMultipart := fileserver.UploadTaskFromMultipart("multipart", req.MultipartForm.File["file"][0])
	tasks := []fileserver.UploadTask{fromPath, fromBytes, fromMultipart, fileserver.UploadTaskFromBytes("empty", nil)}
	s := fileservertest.NewMemStore()
	// 同一任务可重新打开，不复用上次读取位置。
	for range 2 {
		for _, result := range fileserver.UploadAll(t.Context(), s, tasks) {
			if result.Err != nil {
				t.Fatalf("%s: %v", result.Key, result.Err)
			}
		}
		for _, key := range []string{"path", "bytes", "multipart", "empty"} {
			r, err := s.Read(t.Context(), key)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			want := "payload"
			if key == "empty" {
				want = ""
			}
			if string(got) != want {
				t.Fatalf("%s=%q", key, got)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, task := range tasks {
		if _, err := task.Open(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled source: %v", err)
		}
	}
}

func TestUploadSourceErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	if _, err := fileserver.UploadTaskFromPath("k", path).Open(t.Context()); !errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), path) {
		t.Fatalf("path error=%v", err)
	}
	if _, err := fileserver.UploadTaskFromMultipart("k", nil).Open(t.Context()); !errors.Is(err, fileserver.ErrInvalidArgument) {
		t.Fatalf("nil header error=%v", err)
	}
}
