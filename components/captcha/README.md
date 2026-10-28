# captcha

图形验证码：生成（数字 / 字符串）与一次性校验。图片由 [`mojocn/base64Captcha`](https://github.com/mojocn/base64Captcha) v1.3.8 绘制，答案存在 gala 的 [`components/cache`](../cache)（memory 或 redis）里，多实例部署时任意实例出的题都能在任意实例校验。

- 没有全局状态：按需创建 `*Captcha` 实例，像 `cache`、`db` 一样注入使用；不同实例可以有不同的有效期。
- 公开 API 不暴露 base64Captcha 的类型，它只负责画图。
- driver 只构建一次并全局复用：`ConvertFonts` 每次调用都重读字体文件，不复用的话渲染一张验证码图要分配约 11MB。

## 安装

要求 **Go 1.27 或更高版本**。

```sh
go get github.com/apus-run/gala/components/captcha
```

## 接口签名

```go
func NewDigit(c cache.Cache, opts ...Option) *Captcha   // 4 位数字
func NewString(c cache.Cache, opts ...Option) *Captcha  // 4 位小写字母数字
func WithTTL(d time.Duration) Option                    // 默认 DefaultTTL（10 分钟）

// 出题：只返回 id 和 base64 PNG，答案留在服务端
func (c *Captcha) Generate(ctx context.Context) (id, b64s string, err error)

// 校验：总是一次作废
func (c *Captcha) Verify(ctx context.Context, id, code string) (bool, error)
```

## 示例

```go
// 启动时创建一次，注入到 handler
login := captcha.NewDigit(redisCache, captcha.WithTTL(5*time.Minute))

// 出题（登录页展示 b64s 图片）
id, b64s, err := login.Generate(ctx)

// 校验
ok, err := login.Verify(ctx, req.CaptchaID, req.Code)
switch {
case err != nil:
	// cache 后端不可用：按服务故障处理（5xx），不要提示用户验证码错误
case !ok:
	// 答错、过期或已使用：提示用户刷新验证码
}
```

## 校验语义

- **总是一次作废**：答对、答错、空答案、并发重复提交都只有一次机会，防重放也防暴力枚举。多实例共用 redis 时同样成立：只有真正删掉这条记录的那次调用才算数。
- 答案去掉首尾空白，不区分大小写；空答案永远不对，空 id 直接失败且不碰 cache。
- `(false, nil)` 表示答错、过期或已使用；`err != nil` 表示 cache 后端出错，两者可以分别处理。
- `ctx` 一路传到 cache：redis 卡住时由调用方的超时或取消兜底。
- key 带 `captcha:` 前缀，调用方传入的 id 碰不到共享 cache 里的其他 key；`WithTTL` 传 `<=0` 时用默认值，不会变成永不过期。

## 测试

```sh
go test -race ./...
# 多实例共用 redis 的重放测试
GALA_CAPTCHA_REDIS_ADDR=127.0.0.1:6379 go test -race -run Redis ./...
```
