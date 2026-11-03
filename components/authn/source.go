package authn

import (
	"net/http"
	"strings"
)

// TokenSource extracts a raw token from a request. ok is false when the request
// carries no token in this place; validating the token is not its job.
// TokenSource 从请求中提取原始令牌。请求在该位置没有令牌时 ok 为 false；
// 校验令牌不是它的职责。
type TokenSource func(r *http.Request) (token string, ok bool)

// TokenFromHeader returns the Bearer token from the Authorization header
// (RFC 9110 §11.1). Scheme matching is case-insensitive and surrounding
// whitespace around the token is ignored. Another scheme, such as Basic, counts
// as no token.
// TokenFromHeader 返回 Authorization 头里的 Bearer 令牌（RFC 9110 §11.1）。
// scheme 匹配不区分大小写，令牌两侧空白会被忽略。其他 scheme（如 Basic）视同没有令牌。
func TokenFromHeader(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	tok = strings.TrimSpace(tok)
	return tok, tok != ""
}

// TokenFromCookie reads the token from the named cookie. Browsers attach cookies
// to cross-site requests too, so use it only behind CSRF protection.
// TokenFromCookie 从指定名字的 cookie 读取令牌。浏览器在跨站请求里也会带 cookie，
// 所以只应在有 CSRF 防护时使用。
func TokenFromCookie(name string) TokenSource {
	return func(r *http.Request) (string, bool) {
		if r == nil {
			return "", false
		}
		c, err := r.Cookie(name)
		if err != nil {
			return "", false
		}
		tok := strings.TrimSpace(c.Value)
		return tok, tok != ""
	}
}

// TokenFromQuery reads the token from the named query parameter. URLs end up in
// access logs, browser history and Referer headers, so reserve it for clients
// that cannot set headers, such as WebSocket handshakes or download links.
// TokenFromQuery 从指定名字的 query 参数读取令牌。URL 会进入访问日志、浏览器
// 历史和 Referer 头，所以只留给无法设置 header 的客户端（如 WebSocket 握手或下载链接）。
func TokenFromQuery(name string) TokenSource {
	return func(r *http.Request) (string, bool) {
		if r == nil || r.URL == nil {
			return "", false
		}
		tok := strings.TrimSpace(r.URL.Query().Get(name))
		return tok, tok != ""
	}
}

// firstToken returns the token of the first source that has one
// | firstToken 返回第一个持有令牌的 source 的令牌.
func firstToken(r *http.Request, sources []TokenSource) (string, bool) {
	for _, src := range sources {
		if tok, ok := src(r); ok {
			return tok, true
		}
	}
	return "", false
}
