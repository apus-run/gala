package captcha

import (
	"image/color"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mojocn/base64Captcha"
)

// current 是当前使用的 store。不直接改写 base64Captcha.DefaultMemStore：那是第三方包的
// 全局变量，改写它会影响进程里其他用 base64Captcha 的代码，并且与并发读取构成数据竞争。
var current atomic.Pointer[storeHolder]

// storeHolder 让不同具体类型的 Store 都能放进同一个 atomic.Pointer。
type storeHolder struct{ s base64Captcha.Store }

func init() {
	current.Store(&storeHolder{base64Captcha.DefaultMemStore})
}

// SetStore 设置store，可与生成、校验并发调用。
func SetStore(s base64Captcha.Store) {
	current.Store(&storeHolder{s})
}

func store() base64Captcha.Store {
	return current.Load().s
}

// The drivers are built once and shared.
//
// ConvertFonts reads the font files on every call, and the string driver was
// calling it per captcha: eleven megabytes allocated to render one image that
// is a couple of kilobytes. A converted driver is read-only afterwards - it
// holds the parsed fonts and the drawing parameters - so one instance serves
// every request.
//
// The Captcha wrapper is still built per call, because it binds the driver to
// whatever store SetStore has installed, and that can change.
var (
	stringDriverOnce sync.Once
	stringDriver     *base64Captcha.DriverString

	digitDriverOnce sync.Once
	digitDriver     *base64Captcha.DriverDigit
)

func DriverStringFunc() (id, b64s, answer string, err error) {
	stringDriverOnce.Do(func() {
		stringDriver = base64Captcha.NewDriverString(
			46, 140, 2, 2, 4,
			"234567890abcdefghjkmnpqrstuvwxyz",
			&color.RGBA{240, 240, 246, 246},
			nil,
			[]string{"wqy-microhei.ttc"},
		).ConvertFonts()
	})
	captcha := base64Captcha.NewCaptcha(stringDriver, store())
	return captcha.Generate()
}

func DriverDigitFunc() (id, b64s, answer string, err error) {
	digitDriverOnce.Do(func() {
		digitDriver = base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	})
	captcha := base64Captcha.NewCaptcha(digitDriver, store())
	return captcha.Generate()
}

// Verify 校验验证码。code 首尾空白会被去掉；大小写是否敏感由 store 决定
// （默认内存 store 与 NewCacheStore 都不区分大小写）。
//
// id 或 code 为空一律失败：store 对不存在的 id 返回空串，不拦截的话空答案就能通过校验。
// clear=true 时无论答对答错，这个验证码都会作废，防止重放与暴力枚举。
func Verify(id, code string, clear bool) bool {
	code = strings.TrimSpace(code)
	if id == "" || code == "" {
		return false
	}
	return store().Verify(id, code, clear)
}
