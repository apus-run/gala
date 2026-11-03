// Command browser serves an admin console whose session lives in an HttpOnly
// cookie. API clients keep using the Authorization header, and the console
// reads the same session from the cookie.
// Command browser 演示一个管理控制台：会话放在 HttpOnly cookie 里。
// API 客户端继续用 Authorization 头，控制台从 cookie 读取同一个会话。
//
// Cookies are sent on cross-site requests too, so the console also checks a
// double-submit CSRF token on every state-changing request. Without that check
// a malicious page could make a logged-in browser call the API.
// cookie 在跨站请求里也会被发送，因此控制台对每个改状态的请求还要校验
// double-submit CSRF 令牌。没有这道校验，恶意页面就能驱动已登录的浏览器调用 API。
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/authn"
	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/token"
)

const (
	accessCookie = "access"
	csrfCookie   = "csrf"
	csrfHeader   = "X-CSRF-Token"
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

	auth, err := authn.NewSessionAuth(store.NewStore(client, "console:"),
		authn.WithJWTOptions(token.WithSecretKey("replace-with-a-32-byte-or-longer-secret")),
	)
	if err != nil {
		return err
	}

	// AuthFunc tries the Authorization header first, then the session cookie.
	// A bad header fails the request; it never falls back to the cookie.
	// AuthFunc 先尝试 Authorization 头，再尝试会话 cookie。
	// 头是坏的会让请求失败，绝不回退到 cookie。
	authFn := auth.AuthFunc(authn.TokenFromHeader, authn.TokenFromCookie(accessCookie))
	console := authn.NewMiddleware(authFn).Wrap(http.HandlerFunc(consoleHandler))

	// 1. Login sets the session cookie and a readable CSRF cookie.
	// 1. 登录：写入会话 cookie 和一个页面可读的 CSRF cookie。
	pair, err := auth.Issue(ctx, "admin-7")
	if err != nil {
		return err
	}
	csrf := "csrf-value-issued-at-login"
	loginCookies := []*http.Cookie{
		{Name: accessCookie, Value: pair.AccessToken, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, Path: "/"},
		{Name: csrfCookie, Value: csrf, Secure: true, SameSite: http.SameSiteStrictMode, Path: "/"},
	}

	// 2. A page load with the cookie shows the console.
	// 2. 带 cookie 的页面加载能打开控制台。
	if err := do(console, http.MethodGet, "/console", loginCookies, nil, ""); err != nil {
		return fmt.Errorf("page load: %w", err)
	}

	// 3. A state-changing request from the console must echo the CSRF token.
	// 3. 控制台发起的改状态请求必须回传 CSRF 令牌。
	if err := do(console, http.MethodPost, "/console/disable-user", loginCookies, nil, csrf); err != nil {
		return fmt.Errorf("console action with csrf: %w", err)
	}

	// 4. A cross-site form post made by another page: the browser attaches the
	// session cookies, but the attacker cannot read the CSRF cookie to copy it
	// into the header, so the request has no header token and is refused.
	// 4. 其他页面发起的跨站表单提交：浏览器会带上会话 cookie，但攻击者读不到
	// CSRF cookie 也就无法复制进请求头，请求缺少头令牌而被拒绝。
	err = do(console, http.MethodPost, "/console/disable-user", loginCookies, nil, "")
	if !errors.Is(err, errCSRF) {
		return fmt.Errorf("cross-site post was not refused: %v", err)
	}
	fmt.Println("csrf: cross-site post without token refused")

	// 5. An API client sends the same session as a Bearer header and no cookie.
	// The header path needs no CSRF token because browsers never attach it.
	// 5. API 客户端用 Bearer 头发送同一个会话，不带 cookie。
	// 头路径不需要 CSRF 令牌，因为浏览器不会自动带上它。
	if err := do(console, http.MethodPost, "/console/disable-user", nil,
		map[string]string{"Authorization": "Bearer " + pair.AccessToken}, csrf); err != nil {
		return fmt.Errorf("api client with header: %w", err)
	}
	fmt.Println("api: header client disabled user without cookies")
	return nil
}

var errCSRF = errors.New("csrf token missing or mismatched")

func consoleHandler(w http.ResponseWriter, r *http.Request) {
	claims, _ := authn.FromContext[*authn.SessionClaims](r.Context())
	if r.Method == http.MethodPost && isCookieSession(r) {
		requireCSRF(w, r, claims)
		return
	}
	if r.Method == http.MethodPost {
		fmt.Fprintf(w, "disabled user by %s\n", claims.Subject)
		return
	}
	fmt.Fprintf(w, "console for %s\n", claims.Subject)
}

// isCookieSession reports whether the request authenticated with the cookie
// rather than a Bearer header. Only cookie credentials are sent automatically
// by browsers, so only they need the CSRF check.
// isCookieSession 判断请求是用 cookie 还是用 Bearer 头完成认证。
// 只有 cookie 凭据会被浏览器自动发送，所以只有它需要 CSRF 校验。
func isCookieSession(r *http.Request) bool {
	_, hasBearer := authn.TokenFromHeader(r)
	return !hasBearer
}

// requireCSRF compares the header with the CSRF cookie in constant time. The
// cookie is set by the login response and is readable by the page, so a
// cross-site attacker can send it but cannot read it to copy into the header.
// requireCSRF 用常数时间比较请求头与 CSRF cookie。该 cookie 由登录响应写入、
// 页面可读；跨站攻击者能带上它，却读不到、更复制不进请求头。
func requireCSRF(w http.ResponseWriter, r *http.Request, claims *authn.SessionClaims) {
	cookie, err := r.Cookie(csrfCookie)
	header := r.Header.Get(csrfHeader)
	if err != nil || header == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	fmt.Fprintf(w, "disabled user by %s\n", claims.Subject)
}

// do sends one request with the given cookies and an optional extra header map
// and CSRF header value, and maps a 403 from the CSRF check to errCSRF.
// do 发送一次请求，可带给定 cookie、额外的请求头和 CSRF 头值，
// 并把 CSRF 校验产生的 403 映射为 errCSRF。
func do(h http.Handler, method, path string, cookies []*http.Cookie, headers map[string]string, csrf string) error {
	r := httptest.NewRequest(method, path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if csrf != "" {
		r.Header.Set(csrfHeader, csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	switch w.Code {
	case http.StatusOK:
		fmt.Print(w.Body.String())
		return nil
	case http.StatusForbidden:
		return errCSRF
	default:
		return fmt.Errorf("status %d", w.Code)
	}
}
