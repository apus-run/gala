package authn

import (
	"fmt"
	"net/http"
)

// OriginGuard rejects cross-origin state-changing requests. It covers the
// endpoints that run before a session exists or that cannot use the cookie
// carrier's double-submit check — login and logout in particular.
//
// It wraps net/http.CrossOriginProtection, so the rules match Go's own CSRF
// guidance: GET/HEAD/OPTIONS always pass; browsers are judged by
// Sec-Fetch-Site, or by Origin against the request Host; requests carrying
// neither header (non-browser clients) pass; denials answer 403. Trusted
// origins are needed only when the front end is served from a different
// origin than the API.
// OriginGuard 拒绝跨站改状态请求，覆盖会话建立前、或无法使用 cookie 双提交
// 校验的端点——主要是登录与登出。
// 它包装 net/http.CrossOriginProtection，规则与 Go 官方的 CSRF 指引一致：
// GET/HEAD/OPTIONS 永远放行；浏览器按 Sec-Fetch-Site，或 Origin 与请求 Host
// 比对；两者都没有的请求（非浏览器客户端）放行；拒绝时返回 403。只有前端与
// API 不同源时才需要配置信任来源。
type OriginGuard struct {
	protection *http.CrossOriginProtection
}

// NewOriginGuard builds a guard with optional trusted origins, each written as
// scheme://host[:port]. An invalid origin returns an error.
// NewOriginGuard 构建带可选信任来源的守卫，每个来源写成 scheme://host[:port]。
// 非法来源返回错误。
func NewOriginGuard(trustedOrigins ...string) (*OriginGuard, error) {
	protection := http.NewCrossOriginProtection()
	for _, origin := range trustedOrigins {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("authn: invalid trusted origin %q: %w", origin, err)
		}
	}
	// A terse body: the standard library's default explains which header failed,
	// which is useful to an attacker probing the endpoint.
	// 简洁的响应体：标准库默认会说明是哪个头不匹配，这对探测端点的攻击者有用。
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	}))
	return &OriginGuard{protection: protection}, nil
}

// Check reports whether the request passed the cross-origin check. Use it
// inside a handler; Wrap runs the same check before calling the next handler.
// Check 判断请求是否通过跨站校验。在 handler 内部使用；Wrap 会在调用下一个
// handler 前执行同样的检查。
func (g *OriginGuard) Check(r *http.Request) error {
	return g.protection.Check(r)
}

// Wrap rejects cross-origin state-changing requests with 403 before calling
// next.
// Wrap 在调用 next 前用 403 拒绝跨站改状态请求。
func (g *OriginGuard) Wrap(next http.Handler) http.Handler {
	return g.protection.Handler(next)
}
