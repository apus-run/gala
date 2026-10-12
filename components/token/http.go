package token

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// XSRF 用双提交校验：随机值写在可被 JS 读到的 cookie 里，请求时回填到 header，
// 服务端比对两者。cookie 名字沿用前端框架的惯例，省得每个项目自己约定。
const (
	XSRFCookieName = "XSRF-TOKEN"
	XSRFHeaderKey  = "X-XSRF-TOKEN"
)

// SetOption 微调单次 Set 的投递行为。会话级 cookie 取决于这次登录（"记住我"勾没勾），
// 所以是调用参数而不是配置。
type SetOption func(*setOptions)

type setOptions struct {
	sessionOnly bool
}

// WithSessionCookie 这次只写会话级 cookie：不写 Max-Age/Expires，浏览器关掉即登出。
// 只影响浏览器存多久，token 自身的 exp 不变。
func WithSessionCookie() SetOption {
	return func(o *setOptions) {
		o.sessionOnly = true
	}
}

// Set 签发 token 并投递给浏览器：写一个 HttpOnly 的 token cookie 和一个可读的 XSRF
// cookie，两者属性都取自配置，寿命取 token 的 Expiry。返回的 claims 和 token 串同时
// 给 API 客户端用 —— 放进 JSON body 或 Authorization 头即可，不需要 cookie。
// 签发失败时不写任何 cookie。
func (jm *JwtManager[T]) Set(w http.ResponseWriter, userID string, data T, opts ...SetOption) (*Claims[T], string, error) {
	var so setOptions
	for _, opt := range opts {
		opt(&so)
	}

	registered, err := jm.NewRegisteredClaims(userID, time.Now())
	if err != nil {
		return nil, "", err
	}
	claims := &Claims[T]{Data: data, RegisteredClaims: registered}

	tokenStr, err := jm.GenerateClaims(claims)
	if err != nil {
		return nil, "", err
	}

	maxAge := 0 // 0 = 会话 cookie
	if !so.sessionOnly {
		maxAge = int(jm.conf.Expiry.Seconds())
	}
	// token：HttpOnly，JS 拿不到；XSRF：故意让 JS 读得到
	jm.setCookie(w, jm.conf.CookieName, tokenStr, maxAge, true)
	jm.setCookie(w, XSRFCookieName, claims.ID, maxAge, false)

	return claims, tokenStr, nil
}

// Get 从 Authorization: Bearer 或 cookie 取 token 并校验，返回 claims 和原始 token 串。
// 带 cookie 来的请求还要过 XSRF：除安全方法（GET/HEAD/OPTIONS）外，要求
// X-XSRF-TOKEN 与 claims 的 jti 一致 —— cookie 是环境凭据，缺这道校验就是 CSRF。
func (jm *JwtManager[T]) Get(r *http.Request) (*Claims[T], string, error) {
	tokenStr, fromCookie, err := jm.tokenFromRequest(r)
	if err != nil {
		return nil, "", err
	}

	claims := &Claims[T]{}
	if err := jm.ParseClaims(tokenStr, claims); err != nil {
		return nil, "", err
	}

	if fromCookie && !jm.conf.DisableXSRF && !isSafeMethod(r.Method) {
		// claims.ID 为空时必须拒：请求头缺失时 Header.Get 也是空串，两边都空就"相等"了
		if claims.ID == "" || r.Header.Get(XSRFHeaderKey) != claims.ID {
			return nil, "", ErrXSRFMismatch
		}
	}
	return claims, tokenStr, nil
}

// Reset 清掉两个 cookie（登出）。HttpOnly 要和写入时一致，否则浏览器不认这条清除指令。
func (jm *JwtManager[T]) Reset(w http.ResponseWriter) {
	for _, name := range []string{jm.conf.CookieName, XSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", Domain: jm.conf.CookieDomain,
			MaxAge: -1, Expires: time.Unix(0, 0),
			HttpOnly: name == jm.conf.CookieName,
			Secure:   jm.cookieSecure(), SameSite: jm.conf.SameSite,
		})
	}
}

func (jm *JwtManager[T]) setCookie(w http.ResponseWriter, name, value string, maxAge int, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", Domain: jm.conf.CookieDomain,
		MaxAge: maxAge, HttpOnly: httpOnly, Secure: jm.cookieSecure(), SameSite: jm.conf.SameSite,
	})
}

// cookieSecure 取配置，但 SameSite=None 时强制开：浏览器会直接丢掉没有 Secure 的
// SameSite=None cookie，不强制就是"配了却没生效"的静默失败
func (jm *JwtManager[T]) cookieSecure() bool {
	return jm.conf.CookieSecure || jm.conf.SameSite == http.SameSiteNoneMode
}

// tokenFromRequest 取 token：Authorization 头优先，其次 cookie。第二个返回值表示是否来自 cookie。
func (jm *JwtManager[T]) tokenFromRequest(r *http.Request) (string, bool, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, ok := strings.Cut(h, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
			return "", false, fmt.Errorf("%w: Authorization 头应为 Bearer <token>", ErrInvalidToken)
		}
		return strings.TrimSpace(token), false, nil
	}

	c, err := r.Cookie(jm.conf.CookieName)
	if err != nil {
		return "", false, ErrMissingToken
	}
	return c.Value, true, nil
}

// isSafeMethod 是不改状态的方法，双提交 XSRF 对它们没有意义
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
