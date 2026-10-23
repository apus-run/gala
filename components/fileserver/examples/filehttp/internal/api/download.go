package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/apus-run/gala/components/ginx"
)

// Download 按内容哈希下载，支持 HEAD、ETag 与单区间 Range。
func (h *FileHandler) Download(c *ginx.Context) (ginx.Result, error) {
	ctx := c.Request.Context()
	hash := c.Context.Param("hash")
	d, err := h.svc.StatByHash(ctx, hash)
	if err != nil {
		return fail(err)
	}
	// 内容按哈希寻址且不原地修改，属性与后续范围读取对应同一份内容。
	etag := `"` + d.FileHash + `"`
	if matchesETag(strings.Join(c.Request.Header.Values("If-None-Match"), ","), etag) {
		c.Header("ETag", etag)
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusNotModified)
		return ginx.Result{}, ginx.ErrNoResponse
	}
	start, length, status := int64(0), d.FileSize, http.StatusOK
	if c.Request.Method == http.MethodGet {
		if ir := c.GetHeader("If-Range"); ir == "" || ir == etag {
			if off, n, ok := parseRange(c.GetHeader("Range"), d.FileSize); ok {
				if off >= d.FileSize {
					c.Header("Content-Range", fmt.Sprintf("bytes */%d", d.FileSize))
					c.Status(http.StatusRequestedRangeNotSatisfiable)
					return ginx.Result{}, ginx.ErrNoResponse
				}
				start, length, status = off, n, http.StatusPartialContent
			}
		}
	}
	var body io.ReadCloser
	if c.Request.Method == http.MethodGet {
		body, err = h.svc.OpenByHash(ctx, hash, start, length)
		if err != nil {
			return fail(err)
		}
	}
	ct, params, err := mime.ParseMediaType(d.ContentType)
	if err != nil {
		ct, params = "application/octet-stream", nil
	}
	disposition := "attachment"
	if c.Context.Query("inline") == "1" && inlineType(ct) {
		disposition = "inline"
	}
	c.Header("Content-Type", mime.FormatMediaType(ct, params))
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": d.FileName}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
	c.Header("Cache-Control", "no-cache")
	c.Header("ETag", etag)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", strconv.FormatInt(length, 10))
	if status == http.StatusPartialContent {
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, d.FileSize))
	}
	c.Status(status)
	if body == nil { // HEAD 不打开内容流。
		return ginx.Result{}, ginx.ErrNoResponse
	}
	// Storage.Read 的流返回后不随 Context 取消，HTTP 断开时需要主动关闭。
	closeBody := sync.OnceValue(body.Close)
	defer closeBody()
	stop := context.AfterFunc(ctx, func() { _ = closeBody() })
	defer stop()
	c.Writer.WriteHeaderNow()
	_, err = io.CopyN(c.Writer, body, length)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	if err = errors.Join(err, closeBody()); err != nil && !errors.Is(err, context.Canceled) {
		c.Error(err) //nolint:errcheck // 响应已提交，只记录传输错误，不追加 JSON。
	}
	return ginx.Result{}, ginx.ErrNoResponse
}

// 本示例的 ETag 是不含逗号的内容哈希，无需通用的 opaque-tag 解析器。
func matchesETag(value, etag string) bool {
	for _, tag := range strings.Split(value, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}

func inlineType(ct string) bool {
	switch ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/bmp", "image/x-icon",
		"video/mp4", "video/webm", "video/ogg", "audio/mpeg", "audio/mp4", "audio/aac", "audio/wav", "audio/ogg", "audio/webm",
		"application/pdf":
		return true
	}
	return false
}

// parseRange 返回截到对象末尾的单区间。格式错误或多区间回退完整响应；起点越界由调用方返回 416。
func parseRange(value string, size int64) (start, length int64, ok bool) {
	spec, found := strings.CutPrefix(value, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	a, b, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false
	}
	if a == "" {
		n, valid := rangeNumber(b)
		if !valid || n == 0 {
			return 0, 0, false
		}
		return max(size-n, 0), min(n, size), true
	}
	start, ok = rangeNumber(a)
	if !ok {
		return 0, 0, false
	}
	end := size - 1
	if b != "" {
		var valid bool
		end, valid = rangeNumber(b)
		if !valid || end < start {
			return 0, 0, false
		}
	}
	if start >= size {
		return start, 0, true
	}
	// 先截短再计算长度，避免 bytes=0-9223372036854775807 导致加法溢出。
	return start, min(end, size-1) - start + 1, true
}

func rangeNumber(s string) (int64, bool) {
	n, err := strconv.ParseUint(s, 10, 63)
	return int64(n), err == nil
}
