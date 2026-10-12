package token_test

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/apus-run/gala/components/token"
)

// 每个 style 产物的形态：长度、字符集、结构，以及"每次都不一样"
func TestTokenGeneratorStyles(t *testing.T) {
	t.Parallel()

	t.Run("simple 按 SimpleTokenLength 出字符", func(t *testing.T) {
		gen := token.NewTokenGenerator[Payload](token.WithTokenStyle(token.TokenStyleSimple),
			token.WithSimpleTokenLength(24))
		raw, err := gen.GenerateToken("user-1", Payload{})
		if err != nil {
			t.Fatal(err)
		}
		assertCharset(t, raw, 24)
	})

	t.Run("simple 不配长度用默认值", func(t *testing.T) {
		gen := token.NewTokenGenerator[Payload](token.WithTokenStyle(token.TokenStyleSimple))
		raw, err := gen.GenerateToken("user-1", Payload{})
		if err != nil {
			t.Fatal(err)
		}
		assertCharset(t, raw, token.DefaultSimpleLength)
	})

	t.Run("uuid 是 v7", func(t *testing.T) {
		gen := token.NewTokenGenerator[Payload](token.WithTokenStyle(token.TokenStyleUUID))
		raw, err := gen.GenerateToken("user-1", Payload{})
		if err != nil {
			t.Fatal(err)
		}
		u, err := uuid.Parse(raw)
		if err != nil {
			t.Fatalf("不是合法 UUID: %q %v", raw, err)
		}
		if v := u[6] >> 4; v != 7 {
			t.Fatalf("want uuid v7, version nibble = %d", v)
		}
	})

	t.Run("timestamp 形如 毫秒_userID_16位hex", func(t *testing.T) {
		gen := token.NewTokenGenerator[Payload](token.WithTokenStyle(token.TokenStyleTimestamp))
		raw, err := gen.GenerateToken("  user-1  ", Payload{})
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(raw, "_")
		if len(parts) != 3 {
			t.Fatalf("want 3 段, got %d: %q", len(parts), raw)
		}
		// userID 明文可见（文档已注明）：去掉了首尾空白
		if parts[1] != "user-1" {
			t.Fatalf("userID 段 = %q", parts[1])
		}
		millis, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			t.Fatalf("时间戳段不是整数: %q", parts[0])
		}
		if delta := time.Since(time.UnixMilli(millis)); delta > time.Minute || delta < -time.Minute {
			t.Fatalf("时间戳偏差过大: %v", delta)
		}
		if _, err := hex.DecodeString(parts[2]); err != nil || len(parts[2]) != 16 {
			t.Fatalf("随机段应当是 16 位 hex: %q", parts[2])
		}
	})

	t.Run("hash 是 64 位 hex 摘要", func(t *testing.T) {
		gen := token.NewTokenGenerator[Payload](token.WithTokenStyle(token.TokenStyleHash))
		raw, err := gen.GenerateToken("user-1", Payload{})
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) != 64 || !isHex(raw) {
			t.Fatalf("want sha256 hex, got %q", raw)
		}
	})

	t.Run("tiktok 固定 11 位且只用 charset", func(t *testing.T) {
		gen := token.NewTokenGenerator[Payload](token.WithTokenStyle(token.TokenStyleTiktok))
		raw, err := gen.GenerateToken("user-1", Payload{})
		if err != nil {
			t.Fatal(err)
		}
		assertCharset(t, raw, token.TiktokTokenLength)
	})

	t.Run("jwt 走 JwtManager，配置透传到签发与校验", func(t *testing.T) {
		secret := strings.Repeat("s", 32)
		gen := token.NewTokenGenerator[Payload](
			token.WithTokenStyle(token.TokenStyleJWT),
			token.WithJWTSecretKey(secret),
			token.WithJWTIssuer("svc-a"),
			token.WithJWTAudience("api-a"),
		)
		raw, err := gen.GenerateToken("user-1", Payload{SessionID: "s-1"})
		if err != nil {
			t.Fatal(err)
		}

		// TokenConfig 的 JWT* 字段真的进了 token：换个要求 iss/aud 的 manager 也能验过
		manager := token.NewJwtManager[Payload](token.WithSecretKey(secret),
			token.WithIssuer("svc-a"), token.WithAudience("api-a"))
		claims, err := manager.ParseToken(raw)
		if err != nil {
			t.Fatalf("JWT* 配置没有透传: %v", err)
		}
		if claims.Subject != "user-1" || claims.Data.SessionID != "s-1" {
			t.Fatalf("unexpected claims: %+v", claims)
		}
	})

	t.Run("未知 style 报错", func(t *testing.T) {
		gen := token.NewTokenGenerator[Payload](token.WithTokenStyle("nope"))
		if _, err := gen.GenerateToken("user-1", Payload{}); !errors.Is(err, token.ErrUnsupportedTokenStyle) {
			t.Fatalf("want ErrUnsupportedTokenStyle, got %v", err)
		}
	})
}

// 同输入生成多次必须互不相同（随机源真的在用）
func TestTokenGeneratorUniqueness(t *testing.T) {
	t.Parallel()
	const rounds = 64

	for _, style := range []token.TokenStyle{
		token.TokenStyleSimple, token.TokenStyleTimestamp, token.TokenStyleUUID,
		token.TokenStyleHash, token.TokenStyleTiktok,
	} {
		t.Run(string(style), func(t *testing.T) {
			gen := token.NewTokenGenerator[Payload](token.WithTokenStyle(style))
			seen := make(map[string]struct{}, rounds)
			for range rounds {
				raw, err := gen.GenerateToken("user-1", Payload{})
				if err != nil {
					t.Fatal(err)
				}
				if _, dup := seen[raw]; dup {
					t.Fatalf("重复的 token: %q", raw)
				}
				seen[raw] = struct{}{}
			}
		})
	}
}

// 默认值互不共享，选项按顺序覆盖，空值回落
func TestTokenConfigDefaults(t *testing.T) {
	t.Parallel()

	def := token.DefaultTokenConfig()
	if def.TokenStyle != token.TokenStyleJWT || def.JWTIssuer != token.DefaultIssuer ||
		def.JWTAlgorithm != token.DefaultJWTAlgorithm || def.JWTExpiry != token.DefaultJWTExpiry ||
		def.SimpleTokenLength != token.DefaultSimpleLength {
		t.Fatalf("默认值变了: %+v", def)
	}
	if def.JWTSecretKey != "" {
		t.Fatalf("默认密钥应当是空的（没有内置密钥）: %q", def.JWTSecretKey)
	}

	// 两份配置互不影响
	a, b := token.NewTokenConfig(), token.NewTokenConfig()
	a.JWTSecretKey = "changed"
	if b.JWTSecretKey != "" {
		t.Fatal("NewTokenConfig 返回了共享的配置")
	}

	// 后者覆盖前者
	c := token.NewTokenConfig(token.WithJWTSecretKey("a"), token.WithJWTSecretKey("b"))
	if c.JWTSecretKey != "b" {
		t.Fatalf("want b, got %q", c.JWTSecretKey)
	}

	// 空值回落到默认
	c = token.NewTokenConfig(token.WithTokenStyle(""), token.WithSimpleTokenLength(-1))
	if c.TokenStyle != token.TokenStyleJWT || c.SimpleTokenLength != token.DefaultSimpleLength {
		t.Fatalf("空值没有回落: %+v", c)
	}

	// 整份覆盖
	c = token.NewTokenConfig(token.WithTokenConfig(token.TokenConfig{
		JWTSecretKey: "k", TokenStyle: token.TokenStyleUUID, SimpleTokenLength: 8,
	}))
	if c.JWTSecretKey != "k" || c.TokenStyle != token.TokenStyleUUID || c.SimpleTokenLength != 8 {
		t.Fatalf("WithTokenConfig 没有生效: %+v", c)
	}
}

func assertCharset(t *testing.T, raw string, wantLen int) {
	t.Helper()
	if len(raw) != wantLen {
		t.Fatalf("want %d 字符, got %d: %q", wantLen, len(raw), raw)
	}
	for _, r := range raw {
		if !strings.ContainsRune(token.TiktokCharset, r) {
			t.Fatalf("字符 %q 不在 charset 里: %q", r, raw)
		}
	}
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}
