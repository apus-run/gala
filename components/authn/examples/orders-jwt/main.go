// Command ordersjwt shows stateless JWT authentication for a service that is
// called by other services. Each token is valid on its own, so the service
// does not look up a session on every call. A revocation list handles the
// exception: a token from a compromised client must stop working immediately.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/authn"
	"github.com/apus-run/gala/components/authn/store"
	"github.com/apus-run/gala/components/token"
	"github.com/apus-run/gala/pkg/option"
)

// serviceClaims carries the caller identity. The authn package does not
// define business claims; the caller supplies the type.
type serviceClaims struct {
	ClientID string   `json:"client_id"`
	Scopes   []string `json:"scopes"`
	jwt.RegisteredClaims
}

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

	jwtOpts := []option.Option[token.JwtConfig]{
		token.WithSecretKey("replace-with-a-32-byte-or-longer-secret"),
		token.WithIssuer("orders-api"),
	}
	// NewRegisteredClaims gives every token a unique jti. Without it, two tokens
	// minted in the same second would be byte-identical, and revoking one would
	// revoke the other.
	manager := token.NewJwtManager[struct{}](jwtOpts...)
	auth := authn.NewJwtAuth(store.NewStore(client, "revoked:"),
		authn.WithClaims(func() jwt.Claims {
			registered, err := manager.NewRegisteredClaims("billing-service", time.Now())
			if err != nil {
				panic(err) // config error, caught at the first Sign in this example
			}
			return &serviceClaims{ClientID: "billing", RegisteredClaims: registered}
		}),
		authn.WithJWTOptions(jwtOpts...),
	)

	// 1. billing-service asks for a token and calls orders-api.
	issued, err := auth.Sign(ctx)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	fmt.Printf("issued token for billing-service, expires at %d\n", issued.GetExpireAt())

	claims, err := auth.ParseClaims(ctx, issued.GetToken())
	if err != nil {
		return fmt.Errorf("first call: %w", err)
	}
	fmt.Printf("order request accepted for %s\n", claims.(*serviceClaims).Subject)

	// 2. A forged token (changed payload) is rejected by the signature check.
	if _, err := auth.ParseClaims(ctx, tamper(issued.GetToken())); err == nil {
		return errors.New("forged token accepted")
	} else {
		fmt.Println("forged token rejected")
	}

	// 3. The billing key is compromised, so the token is revoked. Revocation
	// is a set of tokens, not a deny-list of clients, so it stops this token
	// at once without touching tokens of other services.
	if err := auth.Destroy(ctx, issued.GetToken()); err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	_, err = auth.ParseClaims(ctx, issued.GetToken())
	switch {
	case errors.Is(err, authn.ErrTokenRevoked):
		fmt.Println("revoked token rejected: key rotation required")
	case err == nil:
		return errors.New("revoked token accepted")
	default:
		return fmt.Errorf("unexpected error after revoke: %w", err)
	}

	// 4. A fresh token issued after the rotation is accepted.
	fresh, err := auth.Sign(ctx)
	if err != nil {
		return fmt.Errorf("sign after rotation: %w", err)
	}
	if _, err := auth.ParseClaims(ctx, fresh.GetToken()); err != nil {
		return fmt.Errorf("fresh token: %w", err)
	}
	fmt.Println("fresh token after rotation accepted")
	return nil
}

// tamper changes the payload of a JWT and leaves the signature as it was.
func tamper(raw string) string {
	return raw[:len(raw)-4] + "AAAA"
}
