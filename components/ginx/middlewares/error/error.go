package error

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/apus-run/gala/pkg/errorsx"
)

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// 下游已经写回响应时，避免重复写入。
		if c.Writer.Written() {
			return
		}

		if len(c.Errors) == 0 {
			return
		}

		appErr := errorsx.FromError(c.Errors.Last().Err).Clone()
		if appErr == nil {
			return
		}

		if appErr.Code < 100 || appErr.Code > 599 {
			appErr.Code = http.StatusInternalServerError
		}

		resp := gin.H{
			"code":    appErr.Code,
			"status":  appErr.Status,
			"message": appErr.Message,
		}
		if appErr.Details != nil {
			resp["details"] = appErr.Details
		}

		c.AbortWithStatusJSON(appErr.Code, resp)
	}
}
