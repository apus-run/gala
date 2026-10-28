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
	"time"

	"github.com/mojocn/base64Captcha"

	"github.com/apus-run/gala/components/cache"
)

// keyPrefix 把验证题限定在自己的命名空间：Verify 的 id 由调用方传入，
// 不加前缀就能指向并删掉共享 cache 里的任意 key。
const keyPrefix = "captcha:q:"

// DefaultTTL 是验证码默认的有效期。
const DefaultTTL = 10 * time.Minute

// Kind 是验证码的类型。
type Kind int

const (
	// Digit 数字验证码：默认 4 位、240×80，无字体依赖。
	Digit Kind = iota
	// String 小写字母数字验证码（去掉了易混的 0/1/i/l/o）：默认 4 位、140×46。
	// 字体在构造时解析（约 11MB 分配）：在启动时创建实例并复用，不要在请求路径上构造。
	String
	// Math 算式验证码（如 "3+5=?"，答案是 8）：默认 140×46，构造时同样解析字体。
	// WithLength 对它无效，算式长度固定。
	// 答案空间很小：每题都答 "0" 约 6.8%（≈1/15）能过，只挡脚本不挡针对性爆破；
	// 登录等要防暴力的场景用 Digit 或 String（4 位 Digit 盲猜是 1/10000）。
	Math
)

// Config 是 Captcha 的配置。
type Config struct {
	// TTL 是答案的有效期，默认 DefaultTTL。
	TTL time.Duration
	// Length 是验证码字符数，默认 4；对 Math 无效。
	Length int
	// Width、Height 是图片像素宽高，默认值按类型：Digit 240×80，String/Math 140×46。
	Width, Height int
}

// Option 配置 Config。
type Option func(*Config)

// DefaultOptions 返回默认配置；宽高留 0，由 New 按类型补默认值。
func DefaultOptions() *Config {
	return &Config{TTL: DefaultTTL, Length: 4}
}

// Apply 从默认配置出发依次应用 opts。
func Apply(opts ...Option) *Config {
	cfg := DefaultOptions()
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

// WithTTL 设置答案有效期；d<=0 时忽略（0 在 memory 与 redis 上都表示永不过期）。
func WithTTL(d time.Duration) Option {
	return func(c *Config) {
		if d > 0 {
			c.TTL = d
		}
	}
}

// WithLength 设置验证码字符数；n<1 时忽略，Math 类型忽略（算式长度固定）。
func WithLength(n int) Option {
	return func(c *Config) {
		if n >= 1 {
			c.Length = n
		}
	}
}

// WithSize 设置图片像素宽高；w 或 h <=0 时忽略（用类型的默认宽高）。
// 字符数加长时记得同步加宽，否则字符挤在一起（宽度不随字符数自动调整）。
func WithSize(width, height int) Option {
	return func(c *Config) {
		if width > 0 && height > 0 {
			c.Width, c.Height = width, height
		}
	}
}

// Captcha 生成并校验一种验证码。零个或多个实例可以共用同一个 cache，
// 方法可以并发调用。
type Captcha struct {
	cache  cache.Cache
	cfg    Config
	driver base64Captcha.Driver
}

// New 按类型创建验证码，c 注入 cache.Cache（memory 或 redis），多实例可共用。
func New(kind Kind, c cache.Cache, opts ...Option) *Captcha {
	cfg := Apply(opts...)
	// WithSize 忽略非法值，宽为 0 即未设置，补类型默认宽高。
	if cfg.Width == 0 {
		switch kind {
		case String, Math:
			cfg.Width, cfg.Height = 140, 46
		default:
			cfg.Width, cfg.Height = 240, 80
		}
	}
	return &Captcha{cache: c, cfg: *cfg, driver: newCaptchaDriver(kind, cfg)}
}

func newCaptchaDriver(kind Kind, cfg *Config) base64Captcha.Driver {
	switch kind {
	case String:
		return base64Captcha.NewDriverString(
			cfg.Height, cfg.Width, 2, 2, cfg.Length,
			"234567890abcdefghjkmnpqrstuvwxyz",
			&color.RGBA{240, 240, 246, 246},
			nil,
			[]string{"wqy-microhei.ttc"},
		)
	case Math:
		return base64Captcha.NewDriverMath(
			cfg.Height, cfg.Width, 2, 2,
			&color.RGBA{240, 240, 246, 246},
			nil,
			[]string{"wqy-microhei.ttc"},
		)
	default:
		return base64Captcha.NewDriverDigit(cfg.Height, cfg.Width, cfg.Length, 0.7, 80)
	}
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
	if err := c.cache.Set(ctx, keyPrefix+id, answer, c.cfg.TTL); err != nil {
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
		return false, nil // 没有题可作废；也避免去碰 "captcha:q:" 这个 key
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
