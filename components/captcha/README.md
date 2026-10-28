# captcha

图形验证码：生成（数字 / 字符串）与一次性校验，建在 [`mojocn/base64Captcha`](https://github.com/mojocn/base64Captcha) v1.3.8 之上。

- driver 只构建一次并全局复用：`ConvertFonts` 每次调用都重读字体文件，不复用的话渲染一张验证码图要分配约 11MB。
- 默认 store 是 base64Captcha 的进程内存，多实例部署或重启即失效；用 `NewCacheStore` 桥到 gala 的 [`components/cache`](github.com/apus-run/gala/components/cache)（memory 或 redis）即可跨实例共享答案。

## 校验语义

- `id` 或答案为空一律失败；答案首尾空白会被去掉，不区分大小写。
- `clear=true` 时验证码一次作废：答错也作废（防暴力枚举），并发校验同一个验证码只有一个能成功，多实例共用 redis 时同样成立。
- `NewCacheStore` 的 key 带 `captcha:` 前缀，调用方传入的 id 碰不到共享 cache 里的其他 key；`expiration<=0` 时用 10 分钟，不会变成永不过期。
- `SetStore` 可以与生成、校验并发调用，不改写 base64Captcha 的全局 `DefaultMemStore`。

## 测试

```sh
go test -race ./...
# 多实例共用 redis 的重放测试
GALA_CAPTCHA_REDIS_ADDR=127.0.0.1:6379 go test -race -run Redis ./...
```

## 安装

要求 **Go 1.27 或更高版本**。

```sh
go get github.com/apus-run/gala/components/captcha
```

## 接口签名

```go
// 生成：返回 id、base64 图片、答案
func DriverDigitFunc() (id, b64s, answer string, err error)
func DriverStringFunc() (id, b64s, answer string, err error)

// 校验（clear=true 表示校验后作废，防重放）
func Verify(id, code string, clear bool) bool

// 换 store：默认是进程内存
func SetStore(s base64Captcha.Store)
func NewCacheStore(c cache.Cache, expiration time.Duration) base64Captcha.Store
```

## 示例

```go
// 启动时装一次 redis 后端
captcha.SetStore(captcha.NewCacheStore(redisCache, 10*time.Minute))

// 出题（登录页展示 b64s 图片，答案留在服务端）
id, b64s, _, err := captcha.DriverDigitFunc()

// 校验（一次作废）
if !captcha.Verify(id, req.Code, true) {
    // 验证码错误
}
```
