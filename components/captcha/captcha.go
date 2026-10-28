// Package captcha 生成图形验证码并做一次性校验。
//
// 图片由 base64Captcha 绘制，答案存在 gala 的 cache.Cache 里（memory 或 redis），
// 因此多实例部署时任意实例出的题都能在任意实例校验。base64Captcha 只用来画图，
// 存储与校验规则都在本包，公开 API 不暴露它的类型。
package captcha

import (
	"context"
	"image/color"
	"strings"
	"sync"
	"time"

	"github.com/mojocn/base64Captcha"

	"github.com/apus-run/gala/components/cache"
)

// keyPrefix 把验证码限定在自己的命名空间：Verify 的 id 由调用方传入，
// 在共享的 cache 里不加前缀就能指向并删掉任意 key。
const keyPrefix = "captcha:"

// DefaultTTL 是验证码默认的有效期。
const DefaultTTL = 10 * time.Minute

// Captcha 生成并校验一种验证码。零个或多个实例可以共用同一个 cache，
// 方法可以并发调用。
type Captcha struct {
	cache  cache.Cache
	driver base64Captcha.Driver
	ttl    time.Duration
}

// Option 配置 Captcha。
type Option func(*Captcha)

// WithTTL 设置验证码有效期；d<=0 时使用 DefaultTTL（0 在 memory 与 redis 上都表示永不过期）。
func WithTTL(d time.Duration) Option {
	return func(c *Captcha) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// NewDigit 返回 4 位数字验证码。
func NewDigit(c cache.Cache, opts ...Option) *Captcha {
	return newCaptcha(c, digitDriver(), opts)
}

// NewString 返回 4 位字母数字验证码（小写，去掉了易混的 0/1/i/l/o）。
func NewString(c cache.Cache, opts ...Option) *Captcha {
	return newCaptcha(c, stringDriver(), opts)
}

func newCaptcha(c cache.Cache, d base64Captcha.Driver, opts []Option) *Captcha {
	cp := &Captcha{cache: c, driver: d, ttl: DefaultTTL}
	for _, opt := range opts {
		opt(cp)
	}
	return cp
}

// Generate 出一道题，返回 id 与 base64 编码的 PNG。答案只留在服务端，不出现在返回值里。
func (c *Captcha) Generate(ctx context.Context) (id, b64s string, err error) {
	id, b64s, _, err = c.generate(ctx)
	return id, b64s, err
}

func (c *Captcha) generate(ctx context.Context) (id, b64s, answer string, err error) {
	id, question, answer := c.driver.GenerateIdQuestionAnswer()
	item, err := c.driver.DrawCaptcha(question)
	if err != nil {
		return "", "", "", err
	}
	if err := c.cache.Set(ctx, keyPrefix+id, answer, c.ttl); err != nil {
		return "", "", "", err
	}
	return id, item.EncodeB64string(), answer, nil
}

// Verify 校验答案，并且总是让这道题作废：答对、答错、空答案、并发重复提交都只有一次机会，
// 多实例共用 redis 时同样成立。答案去掉首尾空白，不区分大小写，空答案永远不对。
//
// 返回 (false, nil) 表示答错、过期或已被使用；err 非空表示 cache 后端不可用，
// 调用方应当按服务故障处理，而不是提示用户验证码错误。
func (c *Captcha) Verify(ctx context.Context, id, code string) (bool, error) {
	if id == "" {
		return false, nil // 没有题可作废；也避免去碰 "captcha:" 这个 key
	}
	code = strings.TrimSpace(code)
	key := keyPrefix + id

	v, getErr := c.cache.Get(ctx, key)
	// 读和删是两步，只有真正删掉这条记录的调用才算数：Deletes 在 memory 下持锁、
	// 在 redis 下是单条 DEL，同一个 key 只会有一个调用拿到 1。
	n, err := c.cache.Deletes(ctx, key)
	if err != nil {
		return false, err
	}
	// getErr 而 Deletes 正常：key 不存在或已过期。
	if getErr != nil || n != 1 {
		return false, nil
	}
	answer, _ := v.(string)
	return code != "" && strings.EqualFold(answer, code), nil
}

// The drivers are built once and shared.
//
// ConvertFonts reads the font files on every call, and the string driver was
// calling it per captcha: eleven megabytes allocated to render one image that
// is a couple of kilobytes. A converted driver is read-only afterwards - it
// holds the parsed fonts and the drawing parameters - so one instance serves
// every Captcha.
var (
	stringDriver = sync.OnceValue(func() base64Captcha.Driver {
		return base64Captcha.NewDriverString(
			46, 140, 2, 2, 4,
			"234567890abcdefghjkmnpqrstuvwxyz",
			&color.RGBA{240, 240, 246, 246},
			nil,
			[]string{"wqy-microhei.ttc"},
		).ConvertFonts()
	})
	digitDriver = sync.OnceValue(func() base64Captcha.Driver {
		return base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	})
)
