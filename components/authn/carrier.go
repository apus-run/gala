package authn

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/apus-run/gala/components/token"
)

const (
	defaultAccessHeader  = "X-Access-Token"
	defaultRefreshHeader = "X-Refresh-Token"
	defaultAccessCookie  = "access_token"
	defaultRefreshCookie = "refresh_token"
)

// TokenCarrier delivers an issued pair to the client and clears it on logout.
// Only the write direction lives here; reading stays with TokenSource, so the
// names configured here must match what the auth middleware reads.
// TokenCarrier 把签发的令牌对交付给客户端，并在登出时清除。
// 这里只有写方向，读取仍归 TokenSource，因此配置的名字要和认证中间件读取的一致。
type TokenCarrier interface {
	// Inject writes the pair to the response | Inject 把令牌对写入响应.
	Inject(w http.ResponseWriter, pair *TokenPair)
	// Clear removes the tokens from the client | Clear 从客户端清除令牌.
	Clear(w http.ResponseWriter)
}

var (
	_ TokenCarrier = (*HeaderCarrier)(nil)
	_ TokenCarrier = (*CookieCarrier)(nil)
)

// HeaderCarrier writes both tokens into response headers; API clients echo the
// access token back as Authorization: Bearer. The zero value uses
// X-Access-Token and X-Refresh-Token.
// HeaderCarrier 把两个令牌写进响应头；API 客户端把访问令牌回填为
// Authorization: Bearer。零值使用 X-Access-Token 与 X-Refresh-Token。
type HeaderCarrier struct {
	// AccessName and RefreshName override the response header names
	// AccessName 与 RefreshName 覆盖响应头名字.
	AccessName  string
	RefreshName string
}

// Inject writes both tokens as response headers.
// Inject 把两个令牌写成响应头。
func (h *HeaderCarrier) Inject(w http.ResponseWriter, pair *TokenPair) {
	if pair == nil {
		return
	}
	w.Header().Set(cmp.Or(h.AccessName, defaultAccessHeader), pair.AccessToken)
	w.Header().Set(cmp.Or(h.RefreshName, defaultRefreshHeader), pair.RefreshToken)
}

// Clear removes the token headers.
// Clear 删除令牌响应头。
func (h *HeaderCarrier) Clear(w http.ResponseWriter) {
	w.Header().Del(cmp.Or(h.AccessName, defaultAccessHeader))
	w.Header().Del(cmp.Or(h.RefreshName, defaultRefreshHeader))
}

// AccessSource reads the access token from the header Inject writes, so a
// client can echo both headers back unchanged.
// AccessSource 从 Inject 写入的头里读取访问令牌，客户端可以把两个头原样回传。
func (h *HeaderCarrier) AccessSource() TokenSource {
	return TokenFromNamedHeader(cmp.Or(h.AccessName, defaultAccessHeader))
}

// RefreshSource reads the refresh token from the header Inject writes.
// RefreshSource 从 Inject 写入的头里读取刷新令牌。
func (h *HeaderCarrier) RefreshSource() TokenSource {
	return TokenFromNamedHeader(cmp.Or(h.RefreshName, defaultRefreshHeader))
}

// CookieCarrier writes both tokens into HttpOnly cookies and a readable XSRF
// cookie for the double-submit check. Inject and Clear rotate the XSRF value on
// every delivery (login, refresh, logout); VerifyCSRF and Protect then reject
// cookie-authenticated state changes that do not echo it. The zero value uses
// access_token / refresh_token, Path "/", and session-level cookies. Names with
// the __Host- / __Secure- prefixes get the prefix rules enforced.
// CookieCarrier 把两个令牌写进 HttpOnly cookie，并额外写一个可读的 XSRF cookie
// 用于双提交校验。Inject 和 Clear 每次交付（登录、刷新、登出）都换新 XSRF 值；
// VerifyCSRF 与 Protect 随后拒绝未回填该值的 cookie 认证改状态请求。
// 零值使用 access_token / refresh_token、Path "/" 和会话级 cookie。
// 带 __Host- / __Secure- 前缀的名字会强制套用前缀规则。
type CookieCarrier struct {
	// AccessName and RefreshName override the cookie names; they must differ
	// AccessName 与 RefreshName 覆盖 cookie 名字；两者必须不同.
	AccessName  string
	RefreshName string
	// Path defaults to "/" | Path 默认为 "/".
	Path string
	// Domain is ignored for __Host- names, which must be host-only
	// Domain 对 __Host- 前缀的名字会被忽略（该前缀要求 host-only）.
	Domain string
	Secure bool
	// SameSite defaults to the browser behavior; use Lax or Strict with cookies
	// SameSite 默认为浏览器行为；接受 cookie 时建议 Lax 或 Strict.
	SameSite http.SameSite
	// MaxAge is the cookie lifetime in seconds; 0 means a session cookie
	// MaxAge 是 cookie 寿命（秒）；0 表示会话 cookie.
	MaxAge int
}

// Inject writes both tokens as cookies plus a fresh XSRF value.
// Inject 把两个令牌写成 cookie，并写入一个新的 XSRF 值。
func (c *CookieCarrier) Inject(w http.ResponseWriter, pair *TokenPair) {
	if pair == nil {
		return
	}
	c.set(w, cmp.Or(c.AccessName, defaultAccessCookie), pair.AccessToken, c.MaxAge, true)
	c.set(w, cmp.Or(c.RefreshName, defaultRefreshCookie), pair.RefreshToken, c.MaxAge, true)
	// The XSRF cookie stays readable on purpose: JS echoes it into X-XSRF-TOKEN.
	// XSRF cookie 刻意可读：JS 要把它回填进 X-XSRF-TOKEN。
	c.set(w, token.XSRFCookieName, rand.Text(), c.MaxAge, false)
}

// Clear expires all three cookies; the attributes must match how they were written.
// Clear 让三个 cookie 过期；属性必须和写入时一致。
func (c *CookieCarrier) Clear(w http.ResponseWriter) {
	c.set(w, cmp.Or(c.AccessName, defaultAccessCookie), "", -1, true)
	c.set(w, cmp.Or(c.RefreshName, defaultRefreshCookie), "", -1, true)
	c.set(w, token.XSRFCookieName, "", -1, false)
}

// AccessSource reads the access cookie, keeping the name in sync with Inject.
// AccessSource 读取访问 cookie，保证名字与 Inject 写入的一致。
func (c *CookieCarrier) AccessSource() TokenSource {
	return TokenFromCookie(cmp.Or(c.AccessName, defaultAccessCookie))
}

// RefreshSource reads the refresh cookie for the refresh endpoint.
// RefreshSource 读取刷新 cookie，供刷新接口使用。
func (c *CookieCarrier) RefreshSource() TokenSource {
	return TokenFromCookie(cmp.Or(c.RefreshName, defaultRefreshCookie))
}

// VerifyCSRF checks the double-submit pair on state-changing requests that
// carry a token cookie. Safe methods and requests without any token cookie
// pass, so header-only API clients are unaffected. A missing cookie or header
// is rejected: a half-cleared browser must not silently lose protection.
// Duplicate XSRF cookies are rejected too — a subdomain that injects its own
// copy would otherwise make the compared value ambiguous. The comparison is
// constant-time, and the Cookie header is parsed once per request.
// VerifyCSRF 对携带令牌 cookie 的改状态请求做双提交校验。
// 安全方法、以及没有任何令牌 cookie 的请求直接通过，纯 Header 的 API 客户端
// 不受影响。cookie 或请求头缺失一律拒绝：清了一半的浏览器不能悄悄失去防护。
// 重复的 XSRF cookie 同样拒绝——否则子域名注入同名 cookie 会让参与比较的值
// 变得有歧义。比较使用常数时间，Cookie 头每个请求只解析一次。
func (c *CookieCarrier) VerifyCSRF(r *http.Request) error {
	if r == nil || isSafeMethod(r.Method) {
		return nil
	}
	accessName := cmp.Or(c.AccessName, defaultAccessCookie)
	refreshName := cmp.Or(c.RefreshName, defaultRefreshCookie)
	var (
		access, refresh bool
		xsrf            *http.Cookie
		duplicate       bool
	)
	for _, cookie := range r.Cookies() {
		switch cookie.Name {
		case accessName:
			access = true
		case refreshName:
			refresh = true
		case token.XSRFCookieName:
			if xsrf != nil {
				duplicate = true
			}
			xsrf = cookie
		}
	}
	if !access && !refresh {
		return nil
	}
	if duplicate || xsrf == nil || xsrf.Value == "" {
		return ErrCSRFMismatch
	}
	if subtle.ConstantTimeCompare([]byte(xsrf.Value), []byte(r.Header.Get(token.XSRFHeaderKey))) != 1 {
		return ErrCSRFMismatch
	}
	return nil
}

// Protect wraps an authentication function with VerifyCSRF, so every route
// guarded by the returned function rejects cookie-authenticated state changes
// that are missing the XSRF header. Authentication runs first: unauthenticated
// requests keep getting 401 rather than 403.
// Protect 用 VerifyCSRF 包装认证函数：由它守护的路由会拒绝缺少 XSRF 头的
// cookie 认证改状态请求。认证先执行：未认证请求仍然得到 401 而不是 403。
func (c *CookieCarrier) Protect(next AuthFunc[*SessionClaims]) AuthFunc[*SessionClaims] {
	return func(ctx context.Context, r *http.Request) (*SessionClaims, error) {
		claims, err := next(ctx, r)
		if err != nil {
			return nil, err
		}
		if err := c.VerifyCSRF(r); err != nil {
			return nil, err
		}
		return claims, nil
	}
}

// set writes one cookie; maxAge < 0 clears it.
// set 写入一个 cookie；maxAge < 0 表示清除。
func (c *CookieCarrier) set(w http.ResponseWriter, name, value string, maxAge int, httpOnly bool) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     cmp.Or(c.Path, "/"),
		Domain:   c.Domain,
		MaxAge:   maxAge,
		HttpOnly: httpOnly,
		// SameSite=None requires Secure; browsers drop the cookie otherwise.
		// SameSite=None 必须带 Secure，否则浏览器直接丢弃 cookie。
		Secure:   c.Secure || c.SameSite == http.SameSiteNoneMode,
		SameSite: c.SameSite,
	}
	// Browsers silently drop cookies that break the prefix rules, which would
	// be a silent loss of protection, so the rules are enforced here.
	// 浏览器会静默丢弃违反前缀规则的 cookie，等于悄悄失去防护，所以这里直接强制。
	switch {
	case strings.HasPrefix(name, "__Host-"):
		cookie.Path, cookie.Domain, cookie.Secure = "/", "", true
	case strings.HasPrefix(name, "__Secure-"):
		cookie.Secure = true
	}
	if maxAge < 0 {
		cookie.Expires = time.Unix(0, 0)
	}
	http.SetCookie(w, cookie)
}

// isSafeMethod reports whether the method does not change state.
// isSafeMethod 报告方法是否不改状态。
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
