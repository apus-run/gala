// Command session walks through a complete login lifecycle with SessionAuth:
// login, API access, token refresh, a replayed refresh token, and logout.
//
// It runs against an in-memory Redis (miniredis) so it needs no setup. In
// production, pass a real *redis.Client to store.NewStore.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/authn"
	"github.com/apus-run/gala/components/authn/store"
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

	auth, err := authn.NewSessionAuth(store.NewStore(client, "session:"),
		authn.WithJWTOptions(
			token.WithSecretKey("replace-with-a-32-byte-or-longer-secret"),
			token.WithIssuer("orders-api"),
		),
		// Short access lifetime makes the refresh step visible in the output.
		authn.WithSessionTTL(2*time.Second, 30*time.Minute),
	)
	if err != nil {
		return err
	}

	// 1. Login. The caller has already checked the password.
	pair, err := auth.Issue(ctx, "user-1001")
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	fmt.Printf("login: access expires in %ds\n", pair.ExpiresIn)

	// 2. Use the API with the access token.
	handler := authn.NewMiddleware(auth.AuthFunc()).Wrap(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			claims, _ := authn.FromContext[*authn.SessionClaims](r.Context())
			fmt.Fprintf(w, "orders for %s\n", claims.Subject)
		}))
	if err := call(handler, pair.AccessToken); err != nil {
		return err
	}

	// 3. The access token expires. The client refreshes with its refresh token.
	time.Sleep(2100 * time.Millisecond)
	if _, err := auth.ParseAccess(ctx, pair.AccessToken); err == nil {
		return errors.New("expired access token was accepted")
	}
	next, err := auth.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		return fmt.Errorf("refresh: %w", err)
	}
	fmt.Println("refresh: new pair issued, old refresh token consumed")

	// 4. Replaying the consumed refresh token is rejected, which signals theft.
	if _, err := auth.Refresh(ctx, pair.RefreshToken); errors.Is(err, authn.ErrSessionInvalid) {
		fmt.Println("replay: old refresh token rejected")
	} else {
		return fmt.Errorf("replay of consumed refresh token: got %v", err)
	}

	// 5. Logout revokes the whole session, including the new pair.
	if err := auth.Logout(ctx, next.AccessToken); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	if err := call(handler, next.AccessToken); err == nil {
		return errors.New("access after logout was accepted")
	}
	fmt.Println("logout: session revoked, API rejects the token")
	return nil
}

// call sends a request through handler with the bearer token and returns an
// error when the response is not 200.
func call(handler http.Handler, accessToken string) error {
	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		return fmt.Errorf("status %d: %s", w.Code, w.Header().Get("WWW-Authenticate"))
	}
	fmt.Print(w.Body.String())
	return nil
}
