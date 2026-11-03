// Package ginx adapts gin handlers to a uniform JSON envelope and error mapping | 为 gin 适配统一的 JSON 响应与错误映射.
//
// # Notes | 说明
//
// The root package only wraps gin: W, B, WC and BC turn business functions
// returning (Result, error) into gin.HandlerFunc and map errors to HTTP status
// via HTTPStatus() or the errorsx code; routing and middlewares live in the
// middlewares subpackages and the application. Claims are passed through
// SetClaims/GetClaims; the token itself is never parsed here.
// 根包只做包装：W、B、WC、BC 把返回 (Result, error) 的业务函数转换成
// gin.HandlerFunc，并按 HTTPStatus() 或 errorsx Code 映射状态码；路由与
// 中间件在 middlewares 子包和应用侧。Claims 经 SetClaims/GetClaims 传递，
// 本包不解析 token。
//
// # Features | 功能
//
// - W/B: plain and request-binding handler wrappers | 无参与绑定请求两种包装
// - WC/BC: claim-aware variants behind SetClaims | 基于 SetClaims 的 claims 变体
// - ErrNoResponse and ErrUnauthorized sentinel handling | ErrNoResponse/ErrUnauthorized 哨兵处理
// - Context wraps gin.Context with typed accessors | Context 提供类型化取参助手
// - Reverse-proxy and static/embed asset helpers | 反向代理与静态资源辅助
//
// # Example | 示例
//
//	r.POST("/login", ginx.B(login)) // func(*ginx.Context, LoginReq) (ginx.Result, error)
//	r.GET("/me", ginx.WC(me))       // requires ginx.SetClaims earlier in the chain
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

// SetClaims stores claims into gin.Context for WC/BC to read
// | SetClaims 将 claims 写入 gin.Context，供 WC/BC 读取。
func SetClaims(ctx *gin.Context, claims func() jwt.Claims) {
	ctx.Set(claimsContextKey{}, claims)
}

// GetClaims reads the claims used by WC/BC from gin.Context; ok is false when
// absent or of the wrong type | GetClaims 从 gin.Context 读取 WC/BC 使用的
// claims；缺失或类型不符时 ok 为 false。
func GetClaims(ctx *gin.Context) (func() jwt.Claims, bool) {
	rawVal, ok := ctx.Get(claimsContextKey{})
	if !ok {
		return nil, false
	}
	claims, ok := rawVal.(func() jwt.Claims)
	return claims, ok
}

// W wraps a handler taking only Context: ErrNoResponse skips the response,
// ErrUnauthorized aborts with 401, other errors map to an HTTP status and
// still write res as JSON | W 包装只接收 Context 的处理函数：
// ErrNoResponse 跳过响应，ErrUnauthorized 以 401 中止，其余错误映射为
// HTTP 状态码但仍以 JSON 写出 res。
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

// B binds the request body into Req via ShouldBind, then behaves like W;
// binding failures abort with 400 | B 先用 ShouldBind 把请求绑定到 Req，
// 之后行为同 W；绑定失败以 400 中止。
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

// WC wraps a claims-aware handler: it reads claims set by SetClaims and
// aborts with 401 when absent | WC 包装带 claims 的处理函数：读取
// SetClaims 写入的 claims，缺失时以 401 中止。
func WC(fn func(*gin.Context, func() jwt.Claims) (Result, error)) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		claims, ok := GetClaims(ctx)
		if !ok {
			slog.Error("无法获得 claims",
				slog.String("path", ctx.Request.URL.Path))
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// TODO observability middlewares could be placed here
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

// BC binds the request body into Req and passes claims, combining B and WC
// | BC 把请求绑定到 Req 并传入 claims，相当于 B 与 WC 的结合。
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

		// TODO observability middlewares could be placed here
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
	if statusErr, ok := errors.AsType[httpStatusError](err); ok {
		code := statusErr.HTTPStatus()
		if code >= 100 && code <= 599 {
			return code
		}
	}
	// Published errorsx v0.8.1 exposes Code but does not yet implement
	// HTTPStatus. Preserve its status when this module is used without go.work.
	// 已发布的 errorsx v0.8.1 暴露 Code 但尚未实现 HTTPStatus；
	// 在不使用 go.work 时保留其状态码。
	if legacyErr, ok := errors.AsType[*errorsx.Error](err); ok && legacyErr != nil && legacyErr.Code >= 100 && legacyErr.Code <= 599 {
		return legacyErr.Code
	}
	return http.StatusInternalServerError
}
