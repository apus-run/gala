package token

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
	"uuid"

	"github.com/apus-run/gala/pkg/option"
)

const (
	// DefaultSimpleLength is the default length of simple tokens | simple 风格的默认长度.
	DefaultSimpleLength = 16
	// TiktokTokenLength is the length of tiktok tokens | tiktok 风格的长度.
	TiktokTokenLength = 11
	// TiktokCharset is the alphabet for random strings | 随机串的字符表.
	TiktokCharset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// HashRandomBytesLen is the random byte count fed into hash tokens | hash 风格的随机字节数.
	HashRandomBytesLen = 16
	// TimestampRandomLen is the random byte count of timestamp tokens | timestamp 风格的随机字节数.
	TimestampRandomLen = 8
)

// NewTokenGenerator creates a generator from options | NewTokenGenerator 从选项创建 TokenGenerator.
func NewTokenGenerator[T any](opts ...option.Option[TokenConfig]) *TokenGenerator[T] {
	return &TokenGenerator[T]{config: NewTokenConfig(opts...)}
}

// GenerateToken mints a token in the configured style | GenerateToken 按配置的风格签发令牌.
func (tg *TokenGenerator[T]) GenerateToken(userID string, data T) (string, error) {
	switch tg.config.TokenStyle {
	case TokenStyleJWT:
		return NewJwtManager[T](tg.config.jwtConfig()).GenerateToken(userID, data)
	case TokenStyleSimple:
		return generateSimpleToken(tg.config.SimpleTokenLength)
	case TokenStyleUUID:
		return generateUUIDToken()
	case TokenStyleTimestamp:
		return generateTimestampToken(userID)
	case TokenStyleHash:
		return generateHashToken(userID)
	case TokenStyleTiktok:
		return generateTiktokToken()
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedTokenStyle, tg.config.TokenStyle)
	}
}

func generateSimpleToken(length int) (string, error) {
	if length <= 0 {
		length = DefaultSimpleLength
	}
	return randomString(length)
}

func generateUUIDToken() (string, error) {
	return uuid.NewV7().String(), nil
}

func generateTimestampToken(userID string) (string, error) {
	randomBytes := make([]byte, TimestampRandomLen)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("%w: %w", ErrGenerateToken, err)
	}

	timestamp := time.Now().UnixMilli()
	randomPart := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%d_%s_%s", timestamp, strings.TrimSpace(userID), randomPart), nil
}

func generateHashToken(userID string) (string, error) {
	randomBytes := make([]byte, HashRandomBytesLen)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("%w: %w", ErrGenerateToken, err)
	}

	data := fmt.Sprintf("%s:%d:%s",
		strings.TrimSpace(userID),
		time.Now().UnixNano(),
		hex.EncodeToString(randomBytes))

	return Hash(data), nil
}

func generateTiktokToken() (string, error) {
	return randomString(TiktokTokenLength)
}

// randomString picks length random characters from TiktokCharset (62 chars).
// randomString 从 TiktokCharset（62 个字符）取 length 个随机字符
func randomString(length int) (string, error) {
	result := make([]byte, length)
	charsetLen := big.NewInt(int64(len(TiktokCharset)))

	for i := range result {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrGenerateToken, err)
		}
		result[i] = TiktokCharset[num.Int64()]
	}

	return string(result), nil
}
