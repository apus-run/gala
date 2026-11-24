// Command browser serves an admin console whose session is delivered as
// HttpOnly cookies. API clients keep using the Authorization header, and the
// console reads the same session from the cookie.
// Command browser 演示一个管理控制台：会话以 HttpOnly cookie 交付。
// API 客户端继续用 Authorization 头，控制台从 cookie 读取同一个会话。
//
// Cookies are sent on cross-site requests too, so the console enables the
// carrier's double-submit CSRF check on every state-changing request. Without
// it a malicious page could make a logged-in browser call the API.
// cookie 在跨站请求里也会被发送，因此控制台对每个改状态的请求启用 carrier 的
// double-submit CSRF 校验。没有这道校验，恶意页面就能驱动已登录的浏览器调用 API。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/authn"
	redistore "github.com/apus-run/gala/components/authn/store/redis"
	"github.com/apus-run/gala/components/token"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	redisServer, err := miniredis.Run()
	if err != nil {
		return err
	}
	defer redisServer.Close()
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer client.Close()

	auth, err := authn.NewSessionAuth(redistore.NewStore(client, "console:"),
		authn.WithJWTOptions(token.WithSecretKey("replace-with-a-32-byte-or-longer-secret")),
	)
	if err != nil {
		return err
	}

	// The carrier delivers the pair as HttpOnly cookies plus a readable XSRF
	// cookie; Protect turns that cookie into a double-submit check.
	// carrier 把令牌对交付为 HttpOnly cookie 和一个可读的 XSRF cookie；
	// Protect 用它做双提交校验。
	carrier := &authn.CookieCarrier{Secure: true, SameSite: http.SameSiteStrictMode}
	// AuthFunc tries the Authorization header first, then the session cookie.
	// A bad header fails the request; it never falls back to the cookie.
	// AuthFunc 先尝试 Authorization 头，再尝试会话 cookie。
	// 头是坏的会让请求失败，绝不回退到 cookie。
	authFn := carrier.Protect(auth.AuthFunc(authn.TokenFromHeader, carrier.AccessSource()))
	console := authn.NewMiddleware(authFn).Wrap(http.HandlerFunc(consoleHandler))

	// 1. Login issues the pair and delivers it through the carrier. The page's
	// JS can read the XSRF cookie and echo it in the request header.
	// 1. 登录签发令牌对并通过 carrier 交付。页面 JS 能读到 XSRF cookie，
	// 并在请求头里回填。
	pair, err := auth.Issue(ctx, "admin-7")
	if err != nil {
		return err
	}
	loginResp := httptest.NewRecorder()
	carrier.Inject(loginResp, pair)
	loginCookies := loginResp.Result().Cookies()
	csrf := cookieValue(loginCookies, token.XSRFCookieName)
	if csrf == "" {
		return errors.New("login response carried no XSRF cookie")
	}

	// 2. A page load with the cookie shows the console.
	// 2. 带 cookie 的页面加载能打开控制台。
	if err := do(console, http.MethodGet, "/console", loginCookies, nil); err != nil {
		return fmt.Errorf("page load: %w", err)
	}

	// 3. A state-changing request from the console echoes the XSRF value.
	// 3. 控制台发起的改状态请求回填 XSRF 值。
	if err := do(console, http.MethodPost, "/console/disable-user", loginCookies,
		map[string]string{token.XSRFHeaderKey: csrf}); err != nil {
		return fmt.Errorf("console action with csrf: %w", err)
	}

	// 4. A cross-site form post made by another page: the browser attaches the
	// session cookies, but the attacker cannot read the XSRF cookie to copy it
	// into the header, so the request has no header token and is refused.
	// 4. 其他页面发起的跨站表单提交：浏览器会带上会话 cookie，但攻击者读不到
	// XSRF cookie 也就无法复制进请求头，请求缺少头令牌而被拒绝。
	err = do(console, http.MethodPost, "/console/disable-user", loginCookies, nil)
	if !errors.Is(err, errForbidden) {
		return fmt.Errorf("cross-site post was not refused: %v", err)
	}
	fmt.Println("csrf: cross-site post without token refused")

	// 5. An API client sends the same session as a Bearer header and no cookie.
	// The header path needs no CSRF token because browsers never attach it.
	// 5. API 客户端用 Bearer 头发送同一个会话，不带 cookie。
	// 头路径不需要 CSRF 令牌，因为浏览器不会自动带上它。
	if err := do(console, http.MethodPost, "/console/disable-user", nil,
		map[string]string{"Authorization": "Bearer " + pair.AccessToken}); err != nil {
		return fmt.Errorf("api client with header: %w", err)
	}
	fmt.Println("api: header client disabled user without cookies")
	return nil
}

func consoleHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := authn.FromContext[*authn.SessionClaims](r.Context())
	if r.Method == http.MethodPost {
		fmt.Fprintf(w, "disabled user by %s\n", claims.Subject)
		return
	}
	fmt.Fprintf(w, "console for %s\n", claims.Subject)
}

// cookieValue reads one cookie the way the page's JS would.
// cookieValue 按页面 JS 的方式读取一个 cookie。
func cookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

var errForbidden = errors.New("request refused")

// do sends one request with the given cookies and headers, and maps a 403 from
// the CSRF check to errForbidden.
// do 发送一次请求，可带给定 cookie 与额外请求头，并把 CSRF 校验产生的 403
// 映射为 errForbidden。
func do(h http.Handler, method, path string, cookies []*http.Cookie, headers map[string]string) error {
	r := httptest.NewRequest(method, path, nil)
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	switch w.Code {
	case http.StatusOK:
		fmt.Print(w.Body.String())
		return nil
	case http.StatusForbidden:
		return errForbidden
	default:
		return fmt.Errorf("status %d", w.Code)
	}
}
