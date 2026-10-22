// Package ui renders the server-side HTML management page with templ + HTMX.
package ui

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/service"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/ui/templates"
	"github.com/apus-run/gala/pkg/errorsx"
)

const (
	// demoUserID 让页面脱离认证服务独立运行；生产应换成 components/authn 的 JWT。
	demoUserID uint64 = 1
	pageSize          = 100
)

// Handler 是 HTML 管理页的适配层：只解析 HTTP、调用 service、渲染 templ。
type Handler struct {
	files     service.FileService
	maxUpload int64
}

// NewHandler 构造 HTML Handler。
func NewHandler(files service.FileService, maxUpload int64) *Handler {
	return &Handler{files: files, maxUpload: maxUpload}
}

// Register 挂载 HTML 路由。JSON API 仍由 api.FileHandler 挂载在 /api 下。
func (h *Handler) Register(r *gin.Engine) {
	r.GET("/", h.page)
	r.GET("/ui/app.js", h.appJS)
	r.GET("/ui/files", h.list)
	r.POST("/ui/files", h.upload)
	r.PUT("/ui/files/:hash", h.rename)
	r.POST("/ui/files/:hash", h.rename) // 无 JS 时的表单回退
	r.DELETE("/ui/files/:hash", h.remove)
}

func (h *Handler) page(c *gin.Context) {
	h.render(c, http.StatusOK, templates.AppPage(templates.AppConfig{
		UserID:     demoUserID,
		MaxUpload:  h.maxUpload,
		MinChunked: service.MinChunkedFileSize,
		ChunkSize:  service.DefaultChunkSize,
	}))
}

func (h *Handler) appJS(c *gin.Context) {
	c.Header("Content-Type", "application/javascript; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	_, _ = c.Writer.WriteString(appJS)
}

func (h *Handler) list(c *gin.Context) {
	h.render(c, http.StatusOK, templates.FileList(h.view(c, "", "")))
}

func (h *Handler) upload(c *gin.Context) {
	// 在解析 multipart 前限制请求体，避免把超大文件全部落盘。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUpload+(1<<20))

	header, err := c.FormFile("file")
	if err != nil {
		h.render(c, http.StatusOK, templates.FileList(h.view(c, "", uploadError(err))))
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}
	src, err := header.Open()
	if err != nil {
		h.render(c, http.StatusOK, templates.FileList(h.view(c, "", "无法读取上传文件")))
		return
	}
	defer func() { _ = src.Close() }()

	res, err := h.files.Upload(c.Request.Context(), demoUserID, c.PostForm("folder"),
		header.Filename, header.Header.Get("Content-Type"), header.Size, src)
	if err != nil {
		log.Printf("ui upload: %v", err)
		h.render(c, http.StatusOK, templates.FileList(h.view(c, "", uiError(err))))
		return
	}
	msg := "上传成功"
	if res.IsDedup {
		msg = "秒传成功：相同内容已存在，已加入你的文件列表"
	}
	h.render(c, http.StatusOK, templates.FileList(h.view(c, msg, "")))
}

func (h *Handler) rename(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("file_name"))
	if err := h.files.Rename(c.Request.Context(), demoUserID, c.Param("hash"), name); err != nil {
		log.Printf("ui rename: %v", err)
		h.render(c, http.StatusOK, templates.FileList(h.view(c, "", uiError(err))))
		return
	}
	h.render(c, http.StatusOK, templates.FileList(h.view(c, "已重命名", "")))
}

func (h *Handler) remove(c *gin.Context) {
	if err := h.files.Remove(c.Request.Context(), demoUserID, c.Param("hash")); err != nil {
		log.Printf("ui delete: %v", err)
		h.render(c, http.StatusOK, templates.FileList(h.view(c, "", uiError(err))))
		return
	}
	h.render(c, http.StatusOK, templates.FileList(h.view(c, "已删除", "")))
}

// view 读取当前用户的文件列表。flash/error 只用于本次渲染。
func (h *Handler) view(c *gin.Context, flash, errMsg string) templates.View {
	files, total, err := h.files.List(c.Request.Context(), demoUserID, "", 1, pageSize)
	if err != nil {
		log.Printf("ui list: %v", err)
		return templates.View{UserID: demoUserID, Flash: flash, Error: "读取文件列表失败"}
	}
	return templates.View{
		Files:  files,
		Total:  total,
		Flash:  flash,
		Error:  errMsg,
		UserID: demoUserID,
	}
}

func (h *Handler) render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		log.Printf("ui render: %v", err)
	}
}

func uploadError(err error) string {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return "文件超过上传上限"
	}
	return "请选择要上传的文件"
}

func uiError(err error) string {
	var ex *errorsx.Error
	if errors.As(err, &ex) {
		switch ex.Code {
		case http.StatusNotFound:
			return "文件不存在，可能已被删除"
		case http.StatusForbidden:
			return "没有权限操作该文件"
		case http.StatusConflict:
			return "文件已被修改，请刷新后重试"
		case http.StatusRequestEntityTooLarge:
			return "文件超过上传上限"
		case http.StatusBadRequest:
			return "文件参数不合法"
		case http.StatusTooManyRequests:
			return "请求过于频繁，请稍后再试"
		}
	}
	switch {
	case errors.Is(err, fileserver.ErrTooLarge):
		return "文件超过上传上限"
	case errors.Is(err, fileserver.ErrInvalidArgument),
		errors.Is(err, fileserver.ErrInvalidKey),
		errors.Is(err, fileserver.ErrSizeMismatch),
		errors.Is(err, fileserver.ErrUnsupported):
		return "文件参数不合法"
	case errors.Is(err, fileserver.ErrNotFound):
		return "文件不存在，可能已被删除"
	case errors.Is(err, fileserver.ErrConflict):
		return "文件已被修改，请刷新后重试"
	case errors.Is(err, fileserver.ErrPermission):
		return "没有权限操作该文件"
	case errors.Is(err, fileserver.ErrIndeterminate):
		return "操作结果不确定，请刷新后核对"
	default:
		return "操作失败，请稍后重试"
	}
}
