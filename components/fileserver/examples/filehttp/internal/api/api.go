// Package api adapts the service layer to HTTP. The layout mirrors the
// file_handler of a classic cloud drive: FileHandler holds two services; the
// public endpoints (download, metadata, public listing — the hash itself is
// the capability token) do no ownership checks, while authenticated endpoints
// get the user injected by userMiddleware and re-checked by each handler.
//
// JSON endpoints go through ginx.W/B (errorsx errors map to HTTP status by
// their Code automatically); uploads read the multipart stream directly, and
// downloads write the response body and return ginx.ErrNoResponse.
// Package api 把 service 适配到 HTTP。结构对照传统网盘的 file_handler：
// FileHandler 持有两个 service，公开接口（下载、元信息、公开列表——哈希即能力
// 凭证）不做归属检查；认证接口经 userMiddleware 注入用户后由各 handler 复核。
//
// JSON 接口走 ginx.W/B（errorsx 错误自动按 Code 映射 HTTP 状态），上传直接读
// multipart 流，下载直接写响应体并返回 ginx.ErrNoResponse。
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/service"
	"github.com/apus-run/gala/components/ginx"
	"github.com/apus-run/gala/pkg/errorsx"
	"github.com/gin-gonic/gin"
)

// FileHandler is the HTTP handler for file endpoints. Handlers only adapt the
// protocol — parse request parameters, call the service, format the response;
// business logic and persistence all sink into the service layer.
// FileHandler 文件相关 HTTP 处理器。Handler 只做协议适配：解析请求参数、
// 调用 service、格式化响应；业务与持久化全部下沉到 service 层。
type FileHandler struct {
	svc       service.FileService
	chunkSvc  service.ChunkService
	maxUpload int64
}

// NewFileHandler constructs a FileHandler | NewFileHandler 构造函数。
func NewFileHandler(svc service.FileService, chunkSvc service.ChunkService, maxUpload int64) *FileHandler {
	return &FileHandler{svc: svc, chunkSvc: chunkSvc, maxUpload: maxUpload}
}

// Register mounts all routes: public and authenticated groups, following the
// router split of the reference implementation.
// Register 挂载全部路由：公开接口与认证接口分组，同参考实现的 router 划分。
func (h *FileHandler) Register(r *gin.Engine) {
	r.Use(gin.Recovery())

	// ---------- Files (public: the hash is the capability token) ----------
	// ---------- 文件相关（公开：哈希即能力凭证） ----------
	files := r.Group("/api/files")
	files.GET("public", ginx.W(h.ListAllFiles))
	files.GET(":hash", ginx.W(h.Download))
	files.HEAD(":hash", ginx.W(h.Download))
	files.GET(":hash/meta", ginx.W(h.GetFileMeta))

	// ---------- Files (authenticated) ----------
	// ---------- 文件相关（需认证） ----------
	fileAuth := r.Group("/api/files", userMiddleware())
	fileAuth.POST("", ginx.W(h.UploadFile))
	fileAuth.GET("", ginx.W(h.ListFiles))
	fileAuth.POST(":hash/replace", ginx.W(h.ReplaceFile))
	fileAuth.PUT(":hash", ginx.B[renameReq](h.FileMetaUpdate))
	fileAuth.DELETE(":hash", ginx.W(h.FileDelete))

	// ---------- Chunked upload (authenticated) ----------
	// ---------- 分块上传（需认证） ----------
	up := r.Group("/api/uploads", userMiddleware())
	up.POST("", ginx.B[initChunkReq](h.InitChunkUpload))
	up.GET(":id", ginx.W(h.GetChunkProgress))
	up.POST(":id/chunks/:index", ginx.W(h.UploadChunk))
	up.POST(":id/complete", ginx.W(h.CompleteChunkUpload))
	up.DELETE(":id", ginx.W(h.CancelUpload))

	// ---------- Folders (authenticated) ----------
	// ---------- 文件夹（需认证） ----------
	folders := r.Group("/api/folders", userMiddleware())
	folders.GET("", ginx.W(h.ListFolders))
	folders.POST("", ginx.B[createFolderReq](h.CreateFolder))
	// The path contains "/", so a wildcard route is used; the root folder
	// cannot be deleted through this endpoint.
	// 路径含 "/"，用通配路由；根目录不允许通过本接口删除。
	folders.DELETE("/*path", ginx.W(h.DeleteFolder))
}

// ListFolders returns the current user's folders (including empty ones) | ListFolders 返回当前用户的文件夹（含空文件夹）。
func (h *FileHandler) ListFolders(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	items, err := h.svc.Folders(c.Request.Context(), uid)
	if err != nil {
		return fail(err)
	}
	return ok("", items)
}

// CreateFolder creates a folder; parent directories exist implicitly | CreateFolder 创建文件夹；父目录隐式存在。
func (h *FileHandler) CreateFolder(c *ginx.Context, req createFolderReq) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	if err := h.svc.CreateFolder(c.Request.Context(), uid, strings.Trim(req.Path, "/")); err != nil {
		return fail(err)
	}
	return ok("文件夹已创建", gin.H{})
}

// DeleteFolder only allows deleting empty folders | DeleteFolder 只允许删除空文件夹。
func (h *FileHandler) DeleteFolder(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	path := strings.TrimPrefix(c.Context.Param("path"), "/")
	if err := h.svc.DeleteFolder(c.Request.Context(), uid, path); err != nil {
		return fail(err)
	}
	return ok("文件夹已删除", gin.H{})
}

// userMiddleware resolves the user from X-User-ID. Production should switch
// to JWT validation from components/authn; the plaintext header exists so the
// example runs standalone without an auth service.
// userMiddleware 从 X-User-ID 解析用户。生产环境应换成 components/authn 的 JWT
// 校验；用明文头是为了让示例脱离认证服务独立运行。
func userMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		v, err := strconv.ParseUint(c.GetHeader("X-User-ID"), 10, 64)
		if err != nil || v == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				ginx.Result{Code: http.StatusUnauthorized, Msg: "请先登录"})
			return
		}
		c.Set("user_id", v)
		c.Next()
	}
}

// mustUser re-checks the user injected by the auth middleware; like the
// reference implementation, every authenticated handler checks it itself to
// guard against a missing middleware.
// mustUser 复核认证中间件注入的用户，与参考实现一样每个需要认证的 handler
// 自行检查，防中间件漏挂。
func mustUser(c *ginx.Context) (uint64, bool) {
	v := c.GetUint64("user_id")
	if v == 0 {
		c.JSON(http.StatusUnauthorized, ginx.Result{Code: http.StatusUnauthorized, Msg: "请先登录"})
		return 0, false
	}
	return v, true
}

// fail assembles the error response body: ginx.W writes the HTTP status from
// the error, and the body carries the business code and message.
// fail 组装错误响应体：ginx.W 用错误里的 HTTP 状态写回，body 带上业务码与消息。
func fail(err error) (ginx.Result, error) {
	res := ginx.Result{Msg: err.Error()}
	var e *errorsx.Error
	if errors.As(err, &e) {
		res.Code = e.Code
	}
	return res, err
}

func ok(msg string, data any) (ginx.Result, error) {
	return ginx.Result{Code: ginx.CodeOK, Msg: msg, Data: data}, nil
}

// ---------- Files ----------
// ---------- 文件相关 ----------

// UploadFile uploads a file (authenticated) | UploadFile 文件上传（需认证）。
func (h *FileHandler) UploadFile(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	header, err := c.FormFile("file")
	if err != nil {
		return fail(errBadRequest("missing multipart field 'file'"))
	}
	if header.Size > h.maxUpload {
		return fail(errTooLarge(h.maxUpload))
	}
	src, err := header.Open()
	if err != nil {
		return fail(errBadRequest("open upload failed"))
	}
	defer src.Close()

	res, err := h.svc.Upload(c.Request.Context(), uid, c.PostForm("folder"), header.Filename,
		header.Header.Get("Content-Type"), header.Size, src)
	if err != nil {
		return fail(err)
	}
	return ok("文件上传成功", res)
}

// ReplaceFile replaces the content of the specified file (authenticated,
// ownership checked): the display name and folder are kept.
// ReplaceFile 用新文件替换指定内容（需认证，校验归属）：保留显示名与目录。
func (h *FileHandler) ReplaceFile(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	header, err := c.FormFile("file")
	if err != nil {
		return fail(errBadRequest("missing multipart field 'file'"))
	}
	if header.Size > h.maxUpload {
		return fail(errTooLarge(h.maxUpload))
	}
	src, err := header.Open()
	if err != nil {
		return fail(errBadRequest("open upload failed"))
	}
	defer src.Close()

	res, err := h.svc.Replace(c.Request.Context(), uid, c.Context.Param("hash"),
		header.Header.Get("Content-Type"), header.Size, src)
	if err != nil {
		return fail(err)
	}
	return ok("已替换为新内容", res)
}

// GetFileMeta returns file metadata (public) | GetFileMeta 获取文件元数据（公开）。
func (h *FileHandler) GetFileMeta(c *ginx.Context) (ginx.Result, error) {
	d, err := h.svc.StatByHash(c.Request.Context(), c.Context.Param("hash"))
	if err != nil {
		return fail(err)
	}
	return ok("", d)
}

// ListFiles returns the current user's file list (authenticated) | ListFiles 获取当前用户的文件列表（需认证）。
func (h *FileHandler) ListFiles(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.svc.List(c.Request.Context(), uid, c.Context.Query("folder"), page, pageSize)
	if err != nil {
		return fail(err)
	}
	return ok("", gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// ListAllFiles returns the public file list across all users | ListAllFiles 获取所有用户的公开文件列表。
func (h *FileHandler) ListAllFiles(c *ginx.Context) (ginx.Result, error) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := h.svc.ListAllFiles(c.Request.Context(), page, pageSize)
	if err != nil {
		return fail(err)
	}
	return ok("", gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// FileMetaUpdate renames a file (authenticated, ownership checked) | FileMetaUpdate 更新文件名称（需认证，校验归属）。
func (h *FileHandler) FileMetaUpdate(c *ginx.Context, req renameReq) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	if err := h.svc.Rename(c.Request.Context(), uid, c.Context.Param("hash"), req.FileName); err != nil {
		return fail(err)
	}
	return ok("更新成功", gin.H{})
}

// FileDelete deletes a file (authenticated, ownership checked; the physical
// object is removed only when the reference count reaches zero).
// FileDelete 删除文件（需认证，校验归属；引用计数归零才删物理对象）。
func (h *FileHandler) FileDelete(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	if err := h.svc.Remove(c.Request.Context(), uid, c.Context.Param("hash")); err != nil {
		return fail(err)
	}
	return ok("文件删除成功", gin.H{})
}

// ---------- Chunked upload endpoints ----------
// ---------- 分块上传接口 ----------

// InitChunkUpload initializes a chunked upload (authenticated) | InitChunkUpload 初始化分块上传（需认证）。
func (h *FileHandler) InitChunkUpload(c *ginx.Context, req initChunkReq) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	res, err := h.chunkSvc.Init(c.Request.Context(), uid,
		req.Folder, req.FileName, req.ContentType, req.FileSize, req.ChunkSize, req.ReplaceOf)
	if err != nil {
		return fail(err)
	}
	return ok("", res)
}

// UploadChunk uploads a single chunk (authenticated). Idempotent: re-uploading
// the same chunk is recognized by the bitmap and skipped.
// UploadChunk 上传单个分块（需认证）。幂等：重复上传同一分块会被位图识别并跳过。
func (h *FileHandler) UploadChunk(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	index, err := strconv.Atoi(c.Context.Param("index"))
	if err != nil || index < 0 {
		return fail(errBadRequest("invalid chunk index"))
	}
	header, err := c.FormFile("chunk")
	if err != nil {
		return fail(errBadRequest("missing multipart field 'chunk'"))
	}
	src, err := header.Open()
	if err != nil {
		return fail(errBadRequest("open chunk failed"))
	}
	defer src.Close()

	if err := h.chunkSvc.UploadChunk(c.Request.Context(), uid, c.Context.Param("id"),
		index, header.Size, src); err != nil {
		return fail(err)
	}
	return ok("分块上传成功", gin.H{"chunk_index": index})
}

// GetChunkProgress queries chunked-upload progress (authenticated, used for
// resuming).
// GetChunkProgress 查询分块上传进度（需认证，用于断点续传）。
func (h *FileHandler) GetChunkProgress(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	p, err := h.chunkSvc.Progress(c.Request.Context(), uid, c.Context.Param("id"))
	if err != nil {
		return fail(err)
	}
	return ok("", p)
}

// CompleteChunkUpload finishes a chunked upload: verifies completeness,
// merges, registers (dedup included) (authenticated).
// CompleteChunkUpload 完成分块上传：校验齐全、合并、登记（含秒传）（需认证）。
func (h *FileHandler) CompleteChunkUpload(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	res, err := h.chunkSvc.Complete(c.Request.Context(), uid, c.Context.Param("id"))
	if err != nil {
		return fail(err)
	}
	return ok("文件上传完成", res)
}

// CancelUpload cancels a chunked upload and cleans up chunks and the session
// (authenticated).
// CancelUpload 取消分块上传，清理分块与会话（需认证）。
func (h *FileHandler) CancelUpload(c *ginx.Context) (ginx.Result, error) {
	uid, valid := mustUser(c)
	if !valid {
		return ginx.Result{}, ginx.ErrNoResponse
	}
	if err := h.chunkSvc.Cancel(c.Request.Context(), uid, c.Context.Param("id")); err != nil {
		return fail(err)
	}
	return ok("已取消", gin.H{})
}

// ---------- Request DTOs and helpers ----------
// ---------- 请求 DTO 与工具 ----------

type renameReq struct {
	FileName string `json:"file_name" binding:"required"`
}

type initChunkReq struct {
	FileName    string `json:"file_name" binding:"required"`
	Folder      string `json:"folder"`
	FileSize    int64  `json:"file_size" binding:"required"`
	ContentType string `json:"content_type"`
	ChunkSize   int64  `json:"chunk_size"`
	ReplaceOf   string `json:"replace_of"` // Non-empty: replace the file with this hash on completion | 非空：完成后替换该哈希的文件。
}

type createFolderReq struct {
	Path string `json:"path" binding:"required"`
}

// errBadRequest / errTooLarge turn HTTP-layer parameter problems into errorsx,
// reusing the unified response shape.
// errBadRequest / errTooLarge 把 HTTP 层的参数问题转为 errorsx，复用统一响应形态。
func errBadRequest(msg string) error {
	return errorsx.BadRequest(errorsx.StatusBadRequest).WithMessage(msg)
}

func errTooLarge(limit int64) error {
	return errorsx.New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE").
		WithMessage(fmt.Sprintf("file exceeds the %d byte limit", limit))
}
