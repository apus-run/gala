// Command browser serves an admin console whose session lives in an HttpOnly
// cookie. API clients keep using the Authorization header, and the console
// reads the same session from the cookie.
//
// Cookies are sent on cross-site requests too, so the console also checks a
// double-submit CSRF token on every state-changing request. Without that check
// a malicious page could make a logged-in browser call the API.
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
	authFn := auth.AuthFunc(authn.TokenFromHeader, authn.TokenFromCookie(accessCookie))
	console := authn.NewMiddleware(authFn).Wrap(http.HandlerFunc(consoleHandler))

	// 1. Login sets the session cookie and a readable CSRF cookie.
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
	if err := do(console, http.MethodGet, "/console", loginCookies, nil, ""); err != nil {
		return fmt.Errorf("page load: %w", err)
	}

	// 3. A state-changing request from the console must echo the CSRF token.
	if err := do(console, http.MethodPost, "/console/disable-user", loginCookies, nil, csrf); err != nil {
		return fmt.Errorf("console action with csrf: %w", err)
	}

	// 4. A cross-site form post made by another page: the browser attaches the
	// session cookies, but the attacker cannot read the CSRF cookie to copy it
	// into the header, so the request has no header token and is refused.
	err = do(console, http.MethodPost, "/console/disable-user", loginCookies, nil, "")
	if !errors.Is(err, errCSRF) {
		return fmt.Errorf("cross-site post was not refused: %v", err)
	}
	fmt.Println("csrf: cross-site post without token refused")

	// 5. An API client sends the same session as a Bearer header and no cookie.
	// The header path needs no CSRF token because browsers never attach it.
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
func isCookieSession(r *http.Request) bool {
	_, hasBearer := authn.TokenFromHeader(r)
	return !hasBearer
}

// requireCSRF compares the header with the CSRF cookie in constant time. The
// cookie is set by the login response and is readable by the page, so a
// cross-site attacker can send it but cannot read it to copy into the header.
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
