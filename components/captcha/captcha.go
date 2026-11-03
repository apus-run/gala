// Package captcha generates image captchas and verifies each answer exactly
// once | 生成图形验证码，并对每道题做一次性校验。
//
// # Notes | 说明
//
// Images are drawn by base64Captcha, but storage and verification live in
// this package: answers are kept in a gala cache.Cache, so with redis any
// instance can verify what any instance issued, and base64Captcha types
// never leak into the public API. The String and Math drivers parse fonts at
// construction (about 11MB): build the [Captcha] once at startup and reuse
// it, never on the request path. Math's answer space is tiny; prefer Digit
// or String where brute force matters.
// 图片由 base64Captcha 绘制，但存储与校验都在本包：答案存在 gala 的
// cache.Cache 里，配 redis 时任意实例出的题都能在任意实例校验，
// base64Captcha 的类型不出现在公开 API 中。String 与 Math 驱动在构造时解析
// 字体（约 11MB）：启动时创建 [Captcha] 并复用，不要在请求路径上构造。
// Math 的答案空间很小，防暴力场景用 Digit 或 String。
//
// # Features | 功能
//
// - Digit, String and Math captcha kinds | Digit 数字、String 字符、Math 算式三种验证码
// - One-shot Verify: wrong, expired or replayed answers return (false, nil); a non-nil error means the cache backend failed | Verify 一次性：答错、过期、重放都返回 (false, nil)；err 非空表示 cache 后端故障
// - Shared cache backend for multi-instance deployments | 共享 cache 后端，支持多实例部署
//
// # Example | 示例
//
//	c := New(Digit, mc)
//	id, b64, err := c.Generate(ctx)
//	ok, err := c.Verify(ctx, id, code)
package captcha

import (
	"context"
	"image/color"
	"strings"
	"time"

	"github.com/mojocn/base64Captcha"

	"github.com/apus-run/gala/components/cache"
)

// keyPrefix namespaces captcha questions: Verify takes a caller-supplied id,
// so without the prefix it could target and delete any key in the shared cache
// | keyPrefix 把验证题限定在自己的命名空间：Verify 的 id 由调用方传入，
// 不加前缀就能指向并删掉共享 cache 里的任意 key。
const keyPrefix = "captcha:q:"

// DefaultTTL is the default answer validity period | DefaultTTL 是验证码默认的有效期.
const DefaultTTL = 10 * time.Minute

// Kind is the captcha type | Kind 是验证码的类型.
type Kind int

const (
	// Digit is a numeric captcha: 4 digits, 240x80 by default, no font needed
	// | Digit 数字验证码：默认 4 位、240×80，无字体依赖.
	Digit Kind = iota
	// String is a lowercase alphanumeric captcha (confusable 0/1/i/l/o removed):
	// 4 chars, 140x46 by default. Fonts are parsed at construction (about an
	// 11MB allocation): build the instance at startup and reuse it, never on
	// the request path
	// | String 小写字母数字验证码（去掉了易混的 0/1/i/l/o）：默认 4 位、140×46。
	// 字体在构造时解析（约 11MB 分配）：在启动时创建实例并复用，不要在请求路径上构造。
	String
	// Math is an arithmetic captcha (e.g. "3+5=?" answering 8): 140x46 by
	// default, and it also parses fonts at construction. WithLength has no
	// effect on it; the expression length is fixed. The answer space is tiny:
	// answering "0" to every question passes about 6.8% (≈1/15) of the time, so
	// it only stops scripts, not targeted brute force; use Digit or String where
	// brute force matters (a blind guess at 4-digit Digit is 1/10000)
	// | Math 算式验证码（如 "3+5=?"，答案是 8）：默认 140×46，构造时同样解析字体。
	// WithLength 对它无效，算式长度固定。
	// 答案空间很小：每题都答 "0" 约 6.8%（≈1/15）能过，只挡脚本不挡针对性爆破；
	// 登录等要防暴力的场景用 Digit 或 String（4 位 Digit 盲猜是 1/10000）。
	Math
)

// Config is the Captcha configuration | Config 是 Captcha 的配置.
type Config struct {
	// TTL is the answer validity period, DefaultTTL by default | TTL 是答案的有效期，默认 DefaultTTL.
	TTL time.Duration
	// Length is the number of captcha characters, 4 by default; no effect on Math | Length 是验证码字符数，默认 4；对 Math 无效.
	Length int
	// Width, Height are the image pixel dimensions, defaults per kind: Digit 240x80, String/Math 140x46 | Width、Height 是图片像素宽高，默认值按类型：Digit 240×80，String/Math 140×46.
	Width, Height int
}

// Option configures Config | Option 配置 Config.
type Option interface {
	apply(*Config)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配为 Option.
type OptionFunc func(*Config)

func (f OptionFunc) apply(o *Config) {
	f(o)
}

// DefaultOptions returns the defaults; width and height stay 0 and New fills
// them per kind | DefaultOptions 返回默认配置；宽高留 0，由 New 按类型补默认值.
func DefaultOptions() *Config {
	return &Config{TTL: DefaultTTL, Length: 4}
}

// NewOptions starts from DefaultOptions and applies opts in order
// | NewOptions 从 DefaultOptions 出发，按顺序应用 opts.
func NewOptions(opts ...Option) *Config {
	o := DefaultOptions()
	o.Apply(opts...)
	return o
}

// Apply applies opts in place in order, without resetting defaults and without
// validation | Apply 按顺序原地应用 opts，不重置默认值、不做校验.
func (o *Config) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// WithTTL sets the answer validity period; d<=0 is ignored (0 means no expiry
// on both memory and redis) | WithTTL 设置答案有效期；d<=0 时忽略（0 在 memory 与 redis 上都表示永不过期）.
func WithTTL(d time.Duration) Option {
	return OptionFunc(func(c *Config) {
		if d > 0 {
			c.TTL = d
		}
	})
}

// WithLength sets the number of captcha characters; n<1 is ignored, and Math
// ignores it (the expression length is fixed) | WithLength 设置验证码字符数；
// n<1 时忽略，Math 类型忽略（算式长度固定）.
func WithLength(n int) Option {
	return OptionFunc(func(c *Config) {
		if n >= 1 {
			c.Length = n
		}
	})
}

// WithSize sets the image pixel dimensions; w or h <=0 is ignored (the kind's
// defaults apply). Widen the image when lengthening the text, or the characters
// crowd together (width does not track character count)
// | WithSize 设置图片像素宽高；w 或 h <=0 时忽略（用类型的默认宽高）。
// 字符数加长时记得同步加宽，否则字符挤在一起（宽度不随字符数自动调整）。
func WithSize(width, height int) Option {
	return OptionFunc(func(c *Config) {
		if width > 0 && height > 0 {
			c.Width, c.Height = width, height
		}
	})
}

// Captcha generates and verifies one captcha kind. Zero or more instances may
// share the same cache; methods are safe for concurrent use
// | Captcha 生成并校验一种验证码。零个或多个实例可以共用同一个 cache，
// 方法可以并发调用。
type Captcha struct {
	cache  cache.Cache
	cfg    Config
	driver base64Captcha.Driver
}

// New creates a captcha of the given kind; c injects the cache.Cache (memory
// or redis), shareable by multiple instances | New 按类型创建验证码，
// c 注入 cache.Cache（memory 或 redis），多实例可共用.
func New(kind Kind, c cache.Cache, opts ...Option) *Captcha {
	cfg := NewOptions(opts...)
	// WithSize ignores invalid values, so a zero width means unset: fill the
	// kind's default dimensions
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

// Generate issues one question and returns its id and the base64-encoded PNG.
// The answer stays server-side and never appears in the return values
// | Generate 出一道题，返回 id 与 base64 编码的 PNG。答案只留在服务端，不出现在返回值里。
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

// Verify checks an answer and always retires the question: a correct answer, a
// wrong one, an empty one and concurrent duplicate submissions all get exactly
// one chance, including with multiple instances sharing redis. The answer is
// trimmed and compared case-insensitively; an empty answer is never correct.
// | Verify 校验答案，并且总是让这道题作废：答对、答错、空答案、并发重复提交都只有一次机会，
// 多实例共用 redis 时同样成立。答案去掉首尾空白，不区分大小写，空答案永远不对。
//
// A (false, nil) return means wrong, expired or already used; a non-nil err
// means the cache backend is unavailable and the caller should treat it as a
// service failure, not tell the user the captcha was wrong.
// 返回 (false, nil) 表示答错、过期或已被使用；err 非空表示 cache 后端不可用，
// 调用方应当按服务故障处理，而不是提示用户验证码错误。
func (c *Captcha) Verify(ctx context.Context, id, code string) (bool, error) {
	if id == "" {
		// nothing to retire; also avoids touching the bare "captcha:q:" key
		// 没有题可作废；也避免去碰 "captcha:q:" 这个 key
		return false, nil
	}
	code = strings.TrimSpace(code)
	key := keyPrefix + id

	v, getErr := c.cache.Get(ctx, key)
	// Read and delete are two steps; only the call that actually deletes the
	// record counts: Deletes holds the lock on memory and is a single DEL on
	// redis, so exactly one caller gets 1 for the same key.
	// 读和删是两步，只有真正删掉这条记录的调用才算数：Deletes 在 memory 下持锁、
	// 在 redis 下是单条 DEL，同一个 key 只会有一个调用拿到 1。
	n, err := c.cache.Deletes(ctx, key)
	if err != nil {
		return false, err
	}
	// getErr with a clean Deletes: the key does not exist or has expired.
	// getErr 而 Deletes 正常：key 不存在或已过期。
	if getErr != nil || n != 1 {
		return false, nil
	}
	answer, _ := v.(string)
	return code != "" && strings.EqualFold(answer, code), nil
}
