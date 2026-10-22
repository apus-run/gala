package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/service"
)

type downloadStub struct {
	service.FileService
	data []byte
}

func (s downloadStub) StatByHash(context.Context, string) (*service.FileDetail, error) {
	return &service.FileDetail{
		FileHash: "hash", FileName: "a.bin", FileSize: int64(len(s.data)),
		ContentType: "application/octet-stream",
	}, nil
}

func (s downloadStub) OpenByHash(_ context.Context, _ string, off, length int64) (io.ReadCloser, error) {
	end := int64(len(s.data))
	if length >= 0 && off+length < end {
		end = off + length
	}
	return io.NopCloser(bytes.NewReader(s.data[off:end])), nil
}

func newDownloadServer(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	data := []byte("0123456789")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewFileHandler(downloadStub{data: data}, nil, 100).Register(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, data
}

func TestDownloadHeadIgnoresRange(t *testing.T) {
	srv, data := newDownloadServer(t)
	req, _ := http.NewRequest(http.MethodHead, srv.URL+"/api/files/hash", nil)
	req.Header.Set("Range", "bytes=0-4")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Length"); got != "10" {
		t.Fatalf("HEAD Content-Length = %q, want 10", got)
	}
	if got := resp.Header.Get("Content-Range"); got != "" {
		t.Fatalf("HEAD Content-Range = %q", got)
	}
	_ = data
}

func TestDownloadUnsatisfiableRange(t *testing.T) {
	srv, _ := newDownloadServer(t)
	for _, h := range []string{"bytes=10-", "bytes=11-", "bytes=99-100"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/files/hash", nil)
		req.Header.Set("Range", h)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
			resp.Body.Close()
			t.Fatalf("%s status = %d, want 416", h, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Range"); got != "bytes */10" {
			resp.Body.Close()
			t.Fatalf("%s Content-Range = %q", h, got)
		}
		resp.Body.Close()
	}
}

func TestDownloadValidRange(t *testing.T) {
	srv, _ := newDownloadServer(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/files/hash", nil)
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range = %q", got)
	}
}
