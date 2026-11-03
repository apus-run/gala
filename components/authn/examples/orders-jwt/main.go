// Command ordersjwt shows stateless JWT authentication for a service that is
// called by other services. Each token is valid on its own, so the service
// does not look up a session on every call. A revocation list handles the
// exception: a token from a compromised client must stop working immediately.
// Command ordersjwt 演示被其他服务调用的服务如何做无状态 JWT 认证。
// 每个令牌自身即有效，服务不必每次调用都查会话。例外情况交给吊销名单：
// 泄密客户端的令牌必须立刻失效。
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
// serviceClaims 携带调用方身份。authn 包不定义业务 claims，
// 类型由调用方提供。
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
	// NewRegisteredClaims 给每个令牌一个唯一的 jti。没有它，同一秒签出的两个令牌
	// 会逐字节相同，吊销其中一个会连带吊销另一个。
	manager := token.NewJwtManager[struct{}](jwtOpts...)
	auth := authn.NewJwtAuth(store.NewStore(client, "revoked:"),
		authn.WithClaims(func() jwt.Claims {
			registered, err := manager.NewRegisteredClaims("billing-service", time.Now())
			if err != nil {
				// config error, caught at the first Sign in this example
				// 配置错误，本示例在第一次 Sign 时就会暴露
				panic(err)
			}
			return &serviceClaims{ClientID: "billing", RegisteredClaims: registered}
		}),
		authn.WithJWTOptions(jwtOpts...),
	)

	// 1. billing-service asks for a token and calls orders-api.
	// 1. billing-service 申请令牌并调用 orders-api。
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
	// 2. 伪造令牌（篡改 payload）会被签名校验拒绝。
	if _, err := auth.ParseClaims(ctx, tamper(issued.GetToken())); err == nil {
		return errors.New("forged token accepted")
	} else {
		fmt.Println("forged token rejected")
	}

	// 3. The billing key is compromised, so the token is revoked. Revocation
	// is a set of tokens, not a deny-list of clients, so it stops this token
	// at once without touching tokens of other services.
	// 3. billing 密钥泄密，令牌被吊销。吊销的是一组令牌而不是客户端黑名单，
	// 因此立刻停掉这个令牌，且不影响其他服务的令牌。
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
	// 4. 轮换密钥后新签的令牌可以通过。
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

// tamper changes the payload of a JWT and leaves the signature as it was
// | tamper 篡改 JWT 的 payload，签名原样保留.
func tamper(raw string) string {
	return raw[:len(raw)-4] + "AAAA"
}
