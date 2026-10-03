package ginx

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/apus-run/gala/pkg/errorsx"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type claimsContextKey struct{}

// SetClaims 将 claims 写入 gin.Context，供 WC/BC 读取。
func SetClaims(ctx *gin.Context, claims func() jwt.Claims) {
	ctx.Set(claimsContextKey{}, claims)
}

// GetClaims 从 gin.Context 读取 WC/BC 使用的 claims。
func GetClaims(ctx *gin.Context) (func() jwt.Claims, bool) {
	rawVal, ok := ctx.Get(claimsContextKey{})
	if !ok {
		return nil, false
	}
	claims, ok := rawVal.(func() jwt.Claims)
	return claims, ok
}

func W(fn func(ctx *Context) (Result, error)) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		res, err := fn(&Context{Context: ctx})
		if errors.Is(err, ErrNoResponse) {
			slog.Debug("不需要响应", slog.Any("err", err))
			return
		}
		if errors.Is(err, ErrUnauthorized) {
			slog.Debug("未授权", slog.Any("err", err))
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if err != nil {
			httpStatus := httpStatusFromError(err)
			slog.Error("执行业务逻辑失败",
				slog.Any("err", err),
				slog.Int("http_status", httpStatus))
			ctx.JSON(httpStatus, res)
			return
		}
		ctx.JSON(http.StatusOK, res)
	}
}

func B[Req any](fn func(ctx *Context, req Req) (Result, error)) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req Req
		if err := ctx.ShouldBind(&req); err != nil {
			slog.Debug("绑定参数失败", slog.Any("err", err))
			ctx.AbortWithStatusJSON(http.StatusBadRequest, Result{
				Code: http.StatusBadRequest,
				Msg:  err.Error(),
				Data: gin.H{},
			})
			return
		}
		res, err := fn(&Context{Context: ctx}, req)
		if errors.Is(err, ErrNoResponse) {
			slog.Debug("不需要响应", slog.Any("err", err))
			return
		}
		if errors.Is(err, ErrUnauthorized) {
			slog.Debug("未授权", slog.Any("err", err))
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if err != nil {
			httpStatus := httpStatusFromError(err)
			slog.Error("执行业务逻辑失败",
				slog.Any("err", err),
				slog.Int("http_status", httpStatus))
			ctx.JSON(httpStatus, res)
			return
		}
		ctx.JSON(http.StatusOK, res)
	}
}

func WC(fn func(*gin.Context, func() jwt.Claims) (Result, error)) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		claims, ok := GetClaims(ctx)
		if !ok {
			slog.Error("无法获得 claims",
				slog.String("path", ctx.Request.URL.Path))
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// TODO 可以在这里放一些可观测性的中间件

		res, err := fn(ctx, claims)
		if errors.Is(err, ErrNoResponse) {
			slog.Debug("不需要响应", slog.Any("err", err))
			return
		}
		if errors.Is(err, ErrUnauthorized) {
			slog.Debug("未授权", slog.Any("err", err))
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if err != nil {
			httpStatus := httpStatusFromError(err)
			slog.Error("执行业务逻辑失败",
				slog.Any("err", err),
				slog.Int("http_status", httpStatus))
			ctx.JSON(httpStatus, res)
			return
		}
		ctx.JSON(http.StatusOK, res)
	}
}

func BC[Req any](fn func(*gin.Context, Req, func() jwt.Claims) (Result, error)) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req Req
		if err := ctx.ShouldBind(&req); err != nil {
			slog.Debug("绑定参数失败", slog.Any("err", err))
			ctx.AbortWithStatusJSON(http.StatusBadRequest, Result{
				Code: http.StatusBadRequest,
				Msg:  err.Error(),
				Data: gin.H{},
			})
			return
		}

		claims, ok := GetClaims(ctx)
		if !ok {
			slog.Error("无法获得 claims",
				slog.String("path", ctx.Request.URL.Path))
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// TODO 可以在这里放一些可观测性的中间件

		res, err := fn(ctx, req, claims)
		if errors.Is(err, ErrNoResponse) {
			slog.Debug("不需要响应", slog.Any("err", err))
			return
		}
		if errors.Is(err, ErrUnauthorized) {
			slog.Debug("未授权", slog.Any("err", err))
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if err != nil {
			httpStatus := httpStatusFromError(err)
			slog.Error("执行业务逻辑失败",
				slog.Any("err", err),
				slog.Int("http_status", httpStatus))
			ctx.JSON(httpStatus, res)
			return
		}
		ctx.JSON(http.StatusOK, res)
	}
}

type httpStatusError interface {
	error
	HTTPStatus() int
}

func httpStatusFromError(err error) int {
	var statusErr httpStatusError
	if errors.As(err, &statusErr) {
		code := statusErr.HTTPStatus()
		if code >= 100 && code <= 599 {
			return code
		}
	}
	// Published errorsx v0.8.1 exposes Code but does not yet implement
	// HTTPStatus. Preserve its status when this module is used without go.work.
	var legacyErr *errorsx.Error
	if errors.As(err, &legacyErr) && legacyErr != nil && legacyErr.Code >= 100 && legacyErr.Code <= 599 {
		return legacyErr.Code
	}
	return http.StatusInternalServerError
}
