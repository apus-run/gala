# captcha

图形验证码：生成（数字 / 字符串 / 算式）与一次性校验。图片由 [`mojocn/base64Captcha`](https://github.com/mojocn/base64Captcha) v1.3.8 绘制，答案存在 gala 的 [`components/cache`](../cache)（memory 或 redis）里，多实例部署时任意实例出的题都能在任意实例校验。

- 没有全局状态：按需创建 `*Captcha` 实例，像 `cache`、`db` 一样注入使用。
- 公开 API 不暴露 base64Captcha 的类型，它只负责画图。
- 字符串/算式验证码的字体在构造时解析（约 11MB 分配）：实例在启动时创建并复用，不要在请求路径上构造；数字 driver 无字体，无此顾虑。字符数、宽高按实例配置（`WithLength` / `WithSize`）。

## 安装

要求 **Go 1.27 或更高版本**。

```sh
go get github.com/apus-run/gala/components/captcha
```

## 接口签名

```go
// 三种类型：Digit 数字（默认 4 位、240×80）、String 小写字母数字（默认 4 位、140×46）、
// Math 算式（"3+5=?"，答案 8，默认 140×46）
func New(kind Kind, c cache.Cache, opts ...Option) *Captcha

// Config + Option：DefaultOptions/Apply 与 WithXxx 逐项覆盖
type Config struct {
	TTL          time.Duration // 答案有效期，默认 DefaultTTL（10 分钟）
	Length       int           // 字符数，默认 4；Math 忽略
	Width, Height int          // 图片像素宽高，默认按类型
}
func WithLength(n int) Option                           // 字符数，n<1 忽略，Math 忽略
func WithSize(width, height int) Option                 // 图片像素宽高，非法值忽略
func WithTTL(d time.Duration) Option                    // 答案有效期

// 出题：只返回 id 和 base64 PNG，答案留在服务端
func (c *Captcha) Generate(ctx context.Context) (id, b64s string, err error)

// 校验：总是一次作废
func (c *Captcha) Verify(ctx context.Context, id, code string) (bool, error)
```

## 示例

对应 gva 的业务配置（`key-long` / `img-width` / `img-height`）：

```go
login := captcha.New(captcha.Digit, cache,
	captcha.WithLength(cfg.KeyLong),
	captcha.WithSize(cfg.ImgWidth, cfg.ImgHeight),
)

id, b64s, err := login.Generate(ctx)

ok, err := login.Verify(ctx, req.CaptchaID, req.Code)
switch {
case err != nil:
	// cache 后端不可用：按服务故障处理（5xx），不要提示用户验证码错误
case !ok:
	// 答错、过期或已使用：提示用户刷新验证码
}
```

`open-captcha` / `open-captcha-timeout`（密码错 N 次后才要求验证码）属于登录流程策略，
不在本组件：用业务自己的 cache 按用户名或 IP 计数失败次数即可（gva 的 `BlackCache` 模式，
约 8 行内联）。

## 防暴力刷的两个层次

1. **答案爆破 → 一次性 Verify**：每道题只有一次提交机会（答对、答错、空答案、并发重复提交都作废），暴力枚举答案不可行；多实例共用 redis 时同样只有一次。前提是单题盲猜概率够低：4 位 Digit 是 1/10000，**Math 例外**——答案集中在少数小整数，每题都答 `0` 约 6.8%（≈1/15）能过，刷十几道题就能蒙中一次。Math 只适合挡脚本，登录等要防暴力的场景用 Digit 或 String。
2. **出题接口刷量 → HTTP 层限流**：`Generate` 渲染 PNG 有成本，匿名可调；用 gala 的 [`components/ratelimit`](../ratelimit) 按 IP 限流出题路由，这属于传输层职责，不内置在本组件。密码爆破的「错 N 次出验证码」同样是业务登录逻辑，见上。

## 校验语义

- **总是一次作废**：只有真正删掉记录的那次调用才算数，`Deletes` 在 memory 下持锁、在 redis 下是单条 DEL。
- 答案去掉首尾空白，不区分大小写；空答案永远不对，空 id 直接失败且不碰 cache。
- `(false, nil)` 表示答错、过期或已使用；`err != nil` 表示 cache 后端出错。
- 题目 key 在 `captcha:q:` 命名空间：调用方传入的 id 碰不到共享 cache 里的其他 key。
- 有效期精度取决于 cache 后端：memory 是秒级，redis 是毫秒级；不要给验证码设置秒级以下的有效期。

## 测试

```sh
go test -race ./...
# 多实例共用 redis 的重放测试
GALA_CAPTCHA_REDIS_ADDR=127.0.0.1:6379 go test -race -run Redis ./...
```
