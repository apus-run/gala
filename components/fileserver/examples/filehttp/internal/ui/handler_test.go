package ui

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/model"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/repo"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/service"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

func newUIServer(t *testing.T) (*httptest.Server, service.FileService) {
	t.Helper()
	provider, err := db.NewDB(sqlite.Open(t.TempDir()+"/ui.db"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := provider.DB(t.Context()).AutoMigrate(
		&model.FileMeta{}, &model.UserFile{}, &model.UserFolder{}, &model.UploadSession{}, &model.StagingObject{},
	); err != nil {
		t.Fatal(err)
	}
	svc := service.NewFileService(repo.New(provider), fileservertest.NewMemStore(), 1<<20)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(svc, 1<<20).Register(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, svc
}

func multipartFile(t *testing.T, name, body string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(fw, body); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestUIPageUploadRenameDelete(t *testing.T) {
	srv, svc := newUIServer(t)
	ctx := context.Background()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "上传文件") {
		t.Fatalf("page status=%d body=%s", resp.StatusCode, page)
	}

	body, contentType := multipartFile(t, "a.txt", "hello ui")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/ui/files", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("HX-Request", "true")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	uploaded, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(uploaded), "上传成功") || !strings.Contains(string(uploaded), "a.txt") {
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, uploaded)
	}

	files, total, err := svc.List(ctx, demoUserID, "", 1, 100)
	if err != nil || total != 1 {
		t.Fatalf("List total=%d err=%v", total, err)
	}
	hash := files[0].FileHash

	form := url.Values{"file_name": {"renamed.txt"}}.Encode()
	req, _ = http.NewRequest(http.MethodPut, srv.URL+"/ui/files/"+hash, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	renamed, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(renamed), "已重命名") || !strings.Contains(string(renamed), "renamed.txt") {
		t.Fatalf("rename body=%s", renamed)
	}

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/ui/files/"+hash, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	deleted, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(deleted), "已删除") || !strings.Contains(string(deleted), "还没有文件") {
		t.Fatalf("delete body=%s", deleted)
	}
}

func TestUIErrorsAreRendered(t *testing.T) {
	srv, _ := newUIServer(t)
	resp, err := http.Post(srv.URL+"/ui/files", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "请选择要上传的文件") {
		t.Fatalf("missing-file body=%s", body)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/ui/files/nope", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "文件不存在") {
		t.Fatalf("missing-delete body=%s", body)
	}
}
