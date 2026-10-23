package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/service"
	"github.com/apus-run/gala/pkg/errorsx"
)

type downloadStub struct {
	service.FileService
	data        []byte
	contentType string
	openErr     error
	closeErr    error
	name        string
	opened      *atomic.Int32
	closed      *atomic.Int32
	body        io.ReadCloser
}

func (s downloadStub) StatByHash(context.Context, string) (*service.FileDetail, error) {
	ct := s.contentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	name := s.name
	if name == "" {
		name = "a.bin"
	}
	return &service.FileDetail{
		FileHash: "hash", FileName: name, FileSize: int64(len(s.data)),
		ContentType: ct,
	}, nil
}

func TestDownloadPreviewTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ct := range []string{"text/html", "image/svg+xml", "image/png"} {
		r := gin.New()
		NewFileHandler(downloadStub{data: []byte("file"), contentType: ct}, nil, 100).Register(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/files/hash?inline=1", nil))
		want := "attachment"
		if ct == "image/png" {
			want = "inline"
		}
		if !strings.HasPrefix(w.Header().Get("Content-Disposition"), want+";") || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("type=%s headers=%v", ct, w.Header())
		}
	}
}

func (s downloadStub) OpenByHash(_ context.Context, _ string, off, length int64) (io.ReadCloser, error) {
	if s.opened != nil {
		s.opened.Add(1)
	}
	if s.openErr != nil {
		return nil, s.openErr
	}
	if s.body != nil {
		return s.body, nil
	}
	end := int64(len(s.data))
	if length >= 0 && off+length < end {
		end = off + length
	}
	return downloadBody{Reader: bytes.NewReader(s.data[off:end]), err: s.closeErr, closed: s.closed}, nil
}

type downloadBody struct {
	io.Reader
	err    error
	closed *atomic.Int32
}

func (b downloadBody) Close() error {
	if b.closed != nil {
		b.closed.Add(1)
	}
	return b.err
}

func TestDownloadDistinguishesOpenAndCommittedErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		stub     downloadStub
		status   int
		wantBody bool
	}{
		{"open failure", downloadStub{openErr: errorsx.NotFound(errorsx.StatusNotFound)}, 404, true},
		{"close failure after empty response", downloadStub{closeErr: errors.New("close failed")}, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			NewFileHandler(tc.stub, nil, 100).Register(r)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/api/files/hash", nil))
			if w.Code != tc.status || (w.Body.Len() > 0) != tc.wantBody {
				t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
			}
			if tc.wantBody && w.Header().Get("Content-Disposition") != "" {
				t.Fatalf("open failure retained file headers: %v", w.Header())
			}
		})
	}
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

func TestDownloadConditionsAndRangeEdges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name               string
		method             string
		headers            map[string]string
		status             int
		body, contentRange string
		noOpen             bool
	}{
		{name: "full", status: 200, body: "0123456789"},
		{name: "head ignores range without opening", method: "HEAD", headers: map[string]string{"Range": "bytes=2-5"}, status: 200, noOpen: true},
		{name: "unsatisfiable without opening", headers: map[string]string{"Range": "bytes=10-"}, status: 416, contentRange: "bytes */10", noOpen: true},
		{name: "suffix", headers: map[string]string{"Range": "bytes=-3"}, status: 206, body: "789", contentRange: "bytes 7-9/10"},
		{name: "large suffix", headers: map[string]string{"Range": "bytes=-100"}, status: 206, body: "0123456789", contentRange: "bytes 0-9/10"},
		{name: "open end", headers: map[string]string{"Range": "bytes=8-"}, status: 206, body: "89", contentRange: "bytes 8-9/10"},
		{name: "end clipped without overflow", headers: map[string]string{"Range": "bytes=0-9223372036854775807"}, status: 206, body: "0123456789", contentRange: "bytes 0-9/10"},
		{name: "malformed", headers: map[string]string{"Range": "bytes=+1-3"}, status: 200, body: "0123456789"},
		{name: "multiple ranges", headers: map[string]string{"Range": "bytes=0-1,3-4"}, status: 200, body: "0123456789"},
		{name: "weak tag in list", headers: map[string]string{"If-None-Match": `"other", W/"hash"`}, status: 304, noOpen: true},
		{name: "tag mismatch", headers: map[string]string{"If-None-Match": `"prefixhash"`}, status: 200, body: "0123456789"},
		{name: "wildcard", headers: map[string]string{"If-None-Match": "*"}, status: 304, noOpen: true},
		{name: "if range matches", headers: map[string]string{"Range": "bytes=2-5", "If-Range": `"hash"`}, status: 206, body: "2345", contentRange: "bytes 2-5/10"},
		{name: "if range mismatch", headers: map[string]string{"Range": "bytes=2-5", "If-Range": `"other"`}, status: 200, body: "0123456789"},
		{name: "if range weak", headers: map[string]string{"Range": "bytes=2-5", "If-Range": `W/"hash"`}, status: 200, body: "0123456789"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opened, closed atomic.Int32
			r := gin.New()
			NewFileHandler(downloadStub{data: []byte("0123456789"), opened: &opened, closed: &closed}, nil, 100).Register(r)
			method := tc.method
			if method == "" {
				method = "GET"
			}
			req := httptest.NewRequest(method, "/api/files/hash", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status || w.Body.String() != tc.body || w.Header().Get("Content-Range") != tc.contentRange {
				t.Fatalf("status=%d body=%q range=%q", w.Code, w.Body.String(), w.Header().Get("Content-Range"))
			}
			wantOpens := int32(1)
			if tc.noOpen {
				wantOpens = 0
			}
			if opened.Load() != wantOpens || closed.Load() != wantOpens {
				t.Fatalf("opened=%d closed=%d", opened.Load(), closed.Load())
			}
		})
	}
}

func TestDownloadTruncatedStreamDoesNotAppendJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	closeErr := errors.New("close failed")
	var transferErr error
	r.Use(func(c *gin.Context) {
		c.Next()
		if last := c.Errors.Last(); last != nil {
			transferErr = last.Err
		}
	})
	NewFileHandler(downloadStub{data: []byte("123"), body: downloadBody{Reader: strings.NewReader("x"), err: closeErr}}, nil, 100).Register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/files/hash", nil))
	if w.Code != 200 || w.Body.String() != "x" || !errors.Is(transferErr, io.ErrUnexpectedEOF) || !errors.Is(transferErr, closeErr) {
		t.Fatalf("status=%d body=%q error=%v", w.Code, w.Body.String(), transferErr)
	}
}

func TestDownloadFilenameAndEmptyFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const name = "中文'\r\n.pdf"
	for _, rangeHeader := range []string{"", "bytes=0-", "bytes=-1"} {
		r := gin.New()
		NewFileHandler(downloadStub{name: name}, nil, 100).Register(r)
		req := httptest.NewRequest("GET", "/api/files/hash", nil)
		req.Header.Set("Range", rangeHeader)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 200
		if rangeHeader != "" {
			want = 416
		}
		if w.Code != want || w.Body.Len() != 0 {
			t.Fatalf("range=%q status=%d body=%q", rangeHeader, w.Code, w.Body.String())
		}
		if rangeHeader == "" {
			kind, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
			if err != nil || kind != "attachment" || params["filename"] != name || strings.ContainsAny(w.Header().Get("Content-Disposition"), "\r\n") {
				t.Fatalf("disposition=%q error=%v", w.Header().Get("Content-Disposition"), err)
			}
		}
	}
}

type blockedDownload struct {
	started, closed chan struct{}
	closes          atomic.Int32
}

func (b *blockedDownload) Read([]byte) (int, error) {
	close(b.started)
	<-b.closed
	return 0, context.Canceled
}
func (b *blockedDownload) Close() error {
	if b.closes.Add(1) == 1 {
		close(b.closed)
	}
	return nil
}

func TestDownloadCancellationClosesStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	b := &blockedDownload{started: make(chan struct{}), closed: make(chan struct{})}
	r := gin.New()
	NewFileHandler(downloadStub{data: []byte("x"), body: b}, nil, 100).Register(r)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/files/hash", nil).WithContext(ctx))
	}()
	select {
	case <-b.started:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not start")
	}
	cancel()
	select {
	case <-done:
		if b.closes.Load() != 1 {
			t.Fatalf("closed %d times", b.closes.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request cancellation left a blocked stream")
	}
}
