package token_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/apus-run/gala/components/token"
)

func newBrowserManager(t *testing.T) *token.JwtManager[Payload] {
	t.Helper()
	return token.NewJwtManager[Payload](
		token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithExpiry(time.Hour),
		token.WithCookieSecure(false), // httptest 走 http
	)
}

// Set 写一对 cookie：token 的 HttpOnly，XSRF 的可被 JS 读到；同时返回 token 串
func TestJwtManagerSetWritesCookiePair(t *testing.T) {
	t.Parallel()
	manager := newBrowserManager(t)

	rr := httptest.NewRecorder()
	claims, raw, err := manager.Set(rr, "user-1", Payload{SessionID: "s-1"})
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.ID == "" {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("want 2 cookies, got %d", len(cookies))
	}
	tokenCookie, xsrfCookie := cookies[0], cookies[1]

	if tokenCookie.Name != token.DefaultCookieName || tokenCookie.Value != raw {
		t.Fatalf("token cookie = %s=%s", tokenCookie.Name, tokenCookie.Value)
	}
	if !tokenCookie.HttpOnly || xsrfCookie.HttpOnly {
		t.Fatal("token 必须 HttpOnly，XSRF 必须让 JS 读得到")
	}
	if xsrfCookie.Name != token.XSRFCookieName || xsrfCookie.Value != claims.ID {
		t.Fatalf("xsrf cookie = %s=%s, jti = %s", xsrfCookie.Name, xsrfCookie.Value, claims.ID)
	}
	for _, c := range cookies {
		if c.MaxAge != 3600 || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.Secure {
			t.Fatalf("cookie 属性不对: %+v", c)
		}
	}

	// 返回的 token 串本身就是可用的 JWT：API 客户端不需要 cookie
	if _, err := manager.ParseToken(raw); err != nil {
		t.Fatalf("Set 返回的 token 应当能直接验: %v", err)
	}
}

// 签发失败不写 cookie
func TestJwtManagerSetFailsClosed(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	if _, _, err := newBrowserManager(t).Set(rr, "   ", Payload{}); !errors.Is(err, token.ErrEmptyUserID) {
		t.Fatalf("want ErrEmptyUserID, got %v", err)
	}
	if len(rr.Result().Cookies()) != 0 {
		t.Fatal("签发失败不该写任何 cookie")
	}
}

// API 客户端：带 Authorization 头，不涉及 XSRF
func TestJwtManagerGetFromAuthorization(t *testing.T) {
	t.Parallel()
	manager := newBrowserManager(t)
	_, raw, err := manager.Set(httptest.NewRecorder(), "user-1", Payload{SessionID: "s-1"})
	if err != nil {
		t.Fatal(err)
	}

	// 用 POST 证明头来源不需要 XSRF
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	claims, got, err := manager.Get(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw || claims.Subject != "user-1" || claims.Data.SessionID != "s-1" {
		t.Fatalf("claims=%+v token=%q", claims, got)
	}

	// scheme 大小写无关
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "bearer "+raw)
	if _, _, err := manager.Get(req); err != nil {
		t.Fatalf("小写 bearer 应当认: %v", err)
	}

	// 格式不对 / 根本没带
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic xyz")
	if _, _, err := manager.Get(req); !errors.Is(err, token.ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
	if _, _, err := manager.Get(httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, token.ErrMissingToken) {
		t.Fatalf("want ErrMissingToken, got %v", err)
	}
}

// 浏览器：cookie 来源要过 XSRF 双提交
func TestJwtManagerGetFromCookieXSRF(t *testing.T) {
	t.Parallel()
	manager := newBrowserManager(t)

	rr := httptest.NewRecorder()
	claims, _, err := manager.Set(rr, "user-1", Payload{SessionID: "s-1"})
	if err != nil {
		t.Fatal(err)
	}
	cookies := rr.Result().Cookies()

	request := func(method, xsrf string) *http.Request {
		req := httptest.NewRequest(method, "/", nil)
		req.AddCookie(cookies[0])
		if xsrf != "" {
			req.Header.Set(token.XSRFHeaderKey, xsrf)
		}
		return req
	}

	// 安全方法不需要 XSRF
	if _, _, err := manager.Get(request(http.MethodGet, "")); err != nil {
		t.Fatalf("GET 不该要求 XSRF: %v", err)
	}

	// 改状态的方法缺 XSRF 头 → 拒
	if _, _, err := manager.Get(request(http.MethodPost, "")); !errors.Is(err, token.ErrXSRFMismatch) {
		t.Fatalf("want ErrXSRFMismatch, got %v", err)
	}

	// 头与 jti 一致 → 过
	if _, _, err := manager.Get(request(http.MethodPost, claims.ID)); err != nil {
		t.Fatalf("XSRF 一致应当通过: %v", err)
	}

	// 头不对 → 拒
	if _, _, err := manager.Get(request(http.MethodPost, "wrong")); !errors.Is(err, token.ErrXSRFMismatch) {
		t.Fatalf("want ErrXSRFMismatch, got %v", err)
	}

	// claims 没有 jti 时必须拒：请求头缺失时 Get 也返回空串，两边都空就"相等"了（曾是绕过）
	noJTI := token.NewJwtManager[Payload](token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithCookieSecure(false))
	raw, err := noJTI.GenerateClaims(&jwt.RegisteredClaims{
		Issuer:    token.DefaultIssuer,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), // 故意没有 ID
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: token.DefaultCookieName, Value: raw})
	if _, _, err := noJTI.Get(req); !errors.Is(err, token.ErrXSRFMismatch) {
		t.Fatalf("没有 jti 的 token 必须拒（CSRF 防护不能形同虚设），got %v", err)
	}

	// 显式关掉（别处已有 CSRF 防护时）
	noXSRF := token.NewJwtManager[Payload](
		token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithCookieSecure(false),
		token.WithDisableXSRF(true),
	)
	if _, _, err := noXSRF.Get(request(http.MethodPost, "")); err != nil {
		t.Fatalf("WithDisableXSRF(true) 之后不该再校验: %v", err)
	}
}

// 会话级 cookie：不写 Max-Age/Expires，浏览器关掉即登出
func TestJwtManagerSetSessionCookie(t *testing.T) {
	t.Parallel()
	manager := newBrowserManager(t)

	rr := httptest.NewRecorder()
	claims, _, err := manager.Set(rr, "user-1", Payload{}, token.WithSessionCookie())
	if err != nil {
		t.Fatal(err)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("want 2 cookies, got %d", len(cookies))
	}
	for _, c := range cookies {
		if c.MaxAge != 0 || !c.Expires.IsZero() {
			t.Fatalf("会话 cookie 不该带寿命: %+v", c)
		}
	}
	// 原文里也不能出现 Max-Age/Expires（Go 对 0 值不输出，这里锁住）
	for _, raw := range rr.Result().Header.Values("Set-Cookie") {
		if strings.Contains(raw, "Max-Age") || strings.Contains(raw, "Expires") {
			t.Fatalf("会话 cookie 原文带了寿命: %s", raw)
		}
	}
	// 只影响浏览器存多久，jti 照旧用于 XSRF
	if cookies[1].Value != claims.ID {
		t.Fatalf("xsrf cookie = %q, jti = %q", cookies[1].Value, claims.ID)
	}
}

// SameSite 用标准库的类型；配 None 时强制带 Secure
func TestJwtManagerSameSite(t *testing.T) {
	t.Parallel()
	type payload struct{}
	setCookie := func(t *testing.T, sameSite http.SameSite, secure bool) string {
		t.Helper()
		manager := token.NewJwtManager[payload](
			token.WithSecretKey(strings.Repeat("s", 32)),
			token.WithCookieSecure(secure),
			token.WithSameSite(sameSite),
		)
		rr := httptest.NewRecorder()
		if _, _, err := manager.Set(rr, "user-1", payload{}); err != nil {
			t.Fatal(err)
		}
		return rr.Result().Header.Values("Set-Cookie")[0]
	}

	if got := setCookie(t, http.SameSiteStrictMode, true); !strings.Contains(got, "SameSite=Strict") {
		t.Fatalf("strict: %s", got)
	}
	// 零值不写属性，由浏览器用它的默认策略
	if got := setCookie(t, 0, true); strings.Contains(got, "SameSite") {
		t.Fatalf("零值不该写 SameSite: %s", got)
	}
	// None 必须带 Secure，否则浏览器直接丢掉这条 cookie —— 配置里 Secure=false 也强制开
	if got := setCookie(t, http.SameSiteNoneMode, false); !strings.Contains(got, "SameSite=None") || !strings.Contains(got, "Secure") {
		t.Fatalf("none 应当强制 Secure: %s", got)
	}
}

func TestJwtManagerResetExpiresCookies(t *testing.T) {
	t.Parallel()
	manager := newBrowserManager(t)

	rr := httptest.NewRecorder()
	manager.Reset(rr)

	cookies := rr.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("want 2 cookies, got %d", len(cookies))
	}
	for _, c := range cookies {
		if c.Value != "" || c.MaxAge >= 0 || c.Path != "/" {
			t.Fatalf("清除 cookie 属性不对: %+v", c)
		}
	}
	// HttpOnly 要和写入时一致，否则浏览器不认这条清除指令
	if !cookies[0].HttpOnly || cookies[1].HttpOnly {
		t.Fatalf("HttpOnly 与写入时不一致: %+v", cookies)
	}
	if cookies[0].Name != token.DefaultCookieName || cookies[1].Name != token.XSRFCookieName {
		t.Fatalf("清的不是同一对 cookie: %+v", cookies)
	}
}

// 过期 token 从 cookie 来也要拒
func TestJwtManagerGetRejectsExpiredCookie(t *testing.T) {
	t.Parallel()
	expired := token.NewJwtManager[Payload](
		token.WithSecretKey(strings.Repeat("s", 32)),
		token.WithCookieSecure(false),
		token.WithExpiry(time.Nanosecond),
	)
	rr := httptest.NewRecorder()
	if _, _, err := expired.Set(rr, "user-1", Payload{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	if _, _, err := expired.Get(req); !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}
