package captcha

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mojocn/base64Captcha"

	"github.com/apus-run/gala/components/cache"
	"github.com/apus-run/gala/components/cache/memory"
	"github.com/apus-run/gala/pkg/lang/value"
)

var ctx = context.Background()

// both 对数字、字符串、算式三种验证码各跑一遍 fn。
func both(t *testing.T, c cache.Cache, fn func(t *testing.T, cp *Captcha)) {
	t.Helper()
	for name, cp := range map[string]*Captcha{"digit": New(Digit, c), "string": New(String, c), "math": New(Math, c)} {
		t.Run(name, func(t *testing.T) { fn(t, cp) })
	}
}

func TestGenerateAndVerify(t *testing.T) {
	both(t, memory.New(), func(t *testing.T, cp *Captcha) {
		id, b64s, answer, err := cp.generate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if id == "" || answer == "" {
			t.Fatalf("empty id or answer: %q %q", id, answer)
		}
		png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(b64s, "data:image/png;base64,"))
		if err != nil || !strings.HasPrefix(string(png), "\x89PNG") {
			t.Fatalf("b64s is not a base64 PNG: err=%v prefix=%q", err, b64s[:min(len(b64s), 32)])
		}

		ok, err := cp.Verify(ctx, id, answer)
		if err != nil || !ok {
			t.Fatalf("Verify(answer) = %v, %v, want true", ok, err)
		}
	})
}

// Generate 不把答案交给调用方。
func TestGenerateDoesNotReturnAnswer(t *testing.T) {
	cp := New(Digit, memory.New())
	id, b64s, err := cp.Generate(ctx)
	if err != nil || id == "" || b64s == "" {
		t.Fatalf("Generate = %q, %d bytes, %v", id, len(b64s), err)
	}
}

// 一次作废：答对之后、答错之后都不能再用。
func TestVerifyIsOneShot(t *testing.T) {
	both(t, memory.New(), func(t *testing.T, cp *Captcha) {
		id, _, answer, _ := cp.generate(ctx)
		if ok, _ := cp.Verify(ctx, id, answer); !ok {
			t.Fatal("first Verify should pass")
		}
		if ok, _ := cp.Verify(ctx, id, answer); ok {
			t.Fatal("a used captcha must not verify again")
		}

		id, _, answer, _ = cp.generate(ctx)
		if ok, _ := cp.Verify(ctx, id, "wrong"); ok {
			t.Fatal("wrong answer verified")
		}
		if ok, _ := cp.Verify(ctx, id, answer); ok {
			t.Fatal("a wrong attempt must spend the captcha (no brute force)")
		}
	})
}

// 空答案永远不对：不拦截时，存的若是空串（或取不到值）空答案就会匹配上。
// 空答案同样让题作废，不给反复试探的机会。
func TestVerifyEmptyAnswer(t *testing.T) {
	c := memory.New()
	cp := New(Digit, c)
	_ = c.Set(ctx, keyPrefix+"blank", "", time.Minute)

	for _, code := range []string{"", "   "} {
		if ok, err := cp.Verify(ctx, "blank", code); ok || err != nil {
			t.Errorf("Verify(blank, %q) = %v, %v, want false, nil", code, ok, err)
		}
	}
	if ok, _ := cp.Verify(ctx, "no-such-id", ""); ok {
		t.Error("empty answer for an unknown id verified")
	}

	id, _, answer, _ := cp.generate(ctx)
	if ok, _ := cp.Verify(ctx, id, ""); ok {
		t.Fatal("empty answer verified")
	}
	if ok, _ := cp.Verify(ctx, id, answer); ok {
		t.Fatal("an empty attempt must spend the captcha too")
	}
}

// 空 id 不碰 cache：否则会去读、去删 "captcha:" 这个 key。
func TestVerifyEmptyID(t *testing.T) {
	c := memory.New()
	_ = c.Set(ctx, keyPrefix, "1234", time.Minute)
	if ok, err := New(Digit, c).Verify(ctx, "", "1234"); ok || err != nil {
		t.Fatalf("Verify(\"\", 1234) = %v, %v, want false, nil", ok, err)
	}
	if !c.Contains(ctx, keyPrefix) {
		t.Fatal("empty id must not delete the bare prefix key")
	}
}

// 去掉首尾空白，不区分大小写（字符串验证码的答案是小写）。
func TestVerifyIgnoresCaseAndSpace(t *testing.T) {
	cp := New(String, memory.New())
	for _, variant := range []func(string) string{
		strings.ToUpper,
		func(s string) string { return " " + s + "\t" },
	} {
		id, _, answer, _ := cp.generate(ctx)
		if ok, err := cp.Verify(ctx, id, variant(answer)); !ok || err != nil {
			t.Fatalf("Verify(%q) as %q = %v, %v", answer, variant(answer), ok, err)
		}
	}
}

// 并发提交同一个验证码只有一次成功。
func TestConcurrentVerifyOnlyOnce(t *testing.T) {
	cp := New(Digit, memory.New())
	for range 50 {
		id, _, answer, _ := cp.generate(ctx)
		if n := verifyConcurrently(32, func(int) bool {
			ok, _ := cp.Verify(ctx, id, answer)
			return ok
		}); n != 1 {
			t.Fatalf("one captcha verified %d times, want 1", n)
		}
	}
}

// 过期后校验失败，且不是后端错误。
func TestVerifyExpired(t *testing.T) {
	cp := New(Digit, memory.New(), WithTTL(time.Second))
	id, _, answer, _ := cp.generate(ctx)
	time.Sleep(2100 * time.Millisecond) // memory 的时间戳精度是秒
	if ok, err := cp.Verify(ctx, id, answer); ok || err != nil {
		t.Fatalf("expired Verify = %v, %v, want false, nil", ok, err)
	}
}

// key 有命名空间：调用方传入的 id 只能命中验证码，碰不到共享 cache 里的其他 key。
func TestKeyNamespace(t *testing.T) {
	c := memory.New()
	if err := c.Set(ctx, "session:42", "secret", time.Minute); err != nil {
		t.Fatal(err)
	}
	cp := New(Digit, c)
	if ok, _ := cp.Verify(ctx, "session:42", "secret"); ok {
		t.Fatal("Verify must not read arbitrary cache keys")
	}
	if v, err := c.Get(ctx, "session:42"); err != nil || v != "secret" {
		t.Fatalf("unrelated key was touched: %v, %v", v, err)
	}

	id, _, _ := cp.Generate(ctx)
	if !c.Contains(ctx, keyPrefix+id) {
		t.Fatal("captcha should be stored under the captcha: prefix")
	}
}

func TestWithTTL(t *testing.T) {
	c := memory.New()
	for _, tc := range []struct {
		opt  []Option
		want time.Duration
	}{
		{nil, DefaultTTL},
		{[]Option{WithTTL(0)}, DefaultTTL},
		{[]Option{WithTTL(-time.Second)}, DefaultTTL},
		{[]Option{WithTTL(time.Minute)}, time.Minute},
	} {
		if got := New(Digit, c, tc.opt...).cfg.TTL; got != tc.want {
			t.Errorf("ttl = %v, want %v", got, tc.want)
		}
	}
}

// 字符数与宽高按实例生效；字体仍只解析一次（stringBase 共享，不在本测试断言）。
func TestWithLengthAndSize(t *testing.T) {
	for name, mk := range map[string]func(cache.Cache) *Captcha{
		"digit":  func(c cache.Cache) *Captcha { return New(Digit, c, WithLength(6), WithSize(200, 70)) },
		"string": func(c cache.Cache) *Captcha { return New(String, c, WithLength(6), WithSize(200, 70)) },
	} {
		t.Run(name, func(t *testing.T) {
			cp := mk(memory.New())
			id, b64s, answer, err := cp.generate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(answer) != 6 {
				t.Fatalf("answer %q has length %d, want 6", answer, len(answer))
			}
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(b64s, "data:image/png;base64,"))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Width != 200 || cfg.Height != 70 {
				t.Fatalf("image = %d×%d, want 200×70", cfg.Width, cfg.Height)
			}
			if ok, err := cp.Verify(ctx, id, answer); !ok || err != nil {
				t.Fatalf("Verify = %v, %v", ok, err)
			}
		})
	}

	// 非法参数回退默认（数字：4 位、240×80）。
	cp := New(Digit, memory.New(), WithLength(0), WithSize(-1, 0))
	if cp.cfg.Length != 4 || cp.cfg.Width != 240 || cp.cfg.Height != 80 {
		t.Fatalf("invalid opts applied: length=%d size=%d×%d", cp.cfg.Length, cp.cfg.Width, cp.cfg.Height)
	}
}

// 实例互不影响：同一个 cache 上的两个 Captcha 各自工作，没有全局状态。
func TestInstancesAreIndependent(t *testing.T) {
	c := memory.New()
	digit, str := New(Digit, c, WithTTL(time.Minute)), New(String, c, WithTTL(time.Hour))
	if digit.cfg.TTL == str.cfg.TTL {
		t.Fatal("options leaked between instances")
	}
	id, _, answer, _ := digit.generate(ctx)
	// 同一个 cache 下 id 通用：用哪个实例校验都行，但只能成功一次。
	if ok, _ := str.Verify(ctx, id, answer); !ok {
		t.Fatal("instances on the same cache should share captchas")
	}
	if ok, _ := digit.Verify(ctx, id, answer); ok {
		t.Fatal("captcha must be spent across instances")
	}

	other := New(Digit, memory.New())
	id, _, answer, _ = digit.generate(ctx)
	if ok, _ := other.Verify(ctx, id, answer); ok {
		t.Fatal("an instance on a different cache must not see the captcha")
	}
}

// ---- 后端故障：必须作为 error 返回，不能伪装成「答错」 ----

var errBackend = errors.New("backend down")

// faultyCache 包装真实 cache，按需让某个操作失败。
type faultyCache struct {
	cache.Cache
	failSet, failGet, failDeletes bool
}

func (f *faultyCache) Set(ctx context.Context, k string, v any, exp time.Duration) error {
	if f.failSet {
		return errBackend
	}
	return f.Cache.Set(ctx, k, v, exp)
}

func (f *faultyCache) Get(ctx context.Context, k string) (any, error) {
	if f.failGet {
		return nil, errBackend
	}
	return f.Cache.Get(ctx, k)
}

func (f *faultyCache) GetAny(ctx context.Context, k string) cache.Value {
	if f.failGet {
		return cache.Value{AnyValue: value.AnyValue{Error: errBackend}}
	}
	return f.Cache.GetAny(ctx, k)
}

func (f *faultyCache) Deletes(ctx context.Context, keys ...string) (int64, error) {
	if f.failDeletes {
		return 0, errBackend
	}
	return f.Cache.Deletes(ctx, keys...)
}

func TestBackendErrors(t *testing.T) {
	t.Run("Generate", func(t *testing.T) {
		cp := New(Digit, &faultyCache{Cache: memory.New(), failSet: true})
		if id, b64s, err := cp.Generate(ctx); !errors.Is(err, errBackend) || id != "" || b64s != "" {
			t.Fatalf("Generate = %q, %d bytes, %v, want empty + errBackend", id, len(b64s), err)
		}
	})
	t.Run("Verify Deletes fails", func(t *testing.T) {
		fc := &faultyCache{Cache: memory.New()}
		cp := New(Digit, fc)
		id, _, answer, _ := cp.generate(ctx)
		fc.failDeletes = true
		if ok, err := cp.Verify(ctx, id, answer); ok || !errors.Is(err, errBackend) {
			t.Fatalf("Verify = %v, %v, want false + errBackend", ok, err)
		}
	})
	t.Run("Verify Get fails", func(t *testing.T) {
		// Get 失败但删除成功：这道题已作废，不能放行，也不应误报为后端错误以外的成功。
		fc := &faultyCache{Cache: memory.New()}
		cp := New(Digit, fc)
		id, _, answer, _ := cp.generate(ctx)
		fc.failGet = true
		if ok, _ := cp.Verify(ctx, id, answer); ok {
			t.Fatal("Verify must not pass when the answer could not be read")
		}
		fc.failGet = false
		if ok, _ := cp.Verify(ctx, id, answer); ok {
			t.Fatal("the captcha must still be spent")
		}
	})
}

// failingDriver 让绘图失败：Generate 必须返回错误，并且不把答案写进 cache。
type failingDriver struct{ base64Captcha.Driver }

func (failingDriver) DrawCaptcha(string) (base64Captcha.Item, error) { return nil, errBackend }

func TestGenerateDrawError(t *testing.T) {
	c := memory.New()
	cp := New(Digit, c)
	cp.driver = failingDriver{cp.driver}
	if id, b64s, err := cp.Generate(ctx); !errors.Is(err, errBackend) || id != "" || b64s != "" {
		t.Fatalf("Generate = %q, %d bytes, %v, want empty + errBackend", id, len(b64s), err)
	}
	if keys := c.Keys(ctx); len(keys) != 0 {
		t.Fatalf("a failed draw left keys behind: %v", keys)
	}
}

// ctx 一路传到 cache：取消的 ctx 让 redis 等后端立刻返回，而不是无限等待。
type ctxCheckingCache struct{ cache.Cache }

func (c ctxCheckingCache) Set(ctx context.Context, k string, v any, exp time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.Cache.Set(ctx, k, v, exp)
}

func (c ctxCheckingCache) Deletes(ctx context.Context, keys ...string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.Cache.Deletes(ctx, keys...)
}

func TestContextIsPropagated(t *testing.T) {
	cp := New(Digit, ctxCheckingCache{memory.New()})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	if _, _, err := cp.Generate(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate with cancelled ctx = %v, want context.Canceled", err)
	}
	id, _, answer, _ := cp.generate(ctx)
	if _, err := cp.Verify(cancelled, id, answer); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify with cancelled ctx = %v, want context.Canceled", err)
	}
}

// 并发生成与校验没有数据竞争（配合 -race），共享的 driver 是只读的。
func TestConcurrentGenerate(t *testing.T) {
	c := memory.New()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, cp := range []*Captcha{New(Digit, c), New(String, c)} {
				id, _, answer, err := cp.generate(ctx)
				if err != nil {
					t.Error(err)
					return
				}
				if ok, err := cp.Verify(ctx, id, answer); !ok || err != nil {
					t.Errorf("Verify = %v, %v", ok, err)
				}
			}
		}()
	}
	wg.Wait()
}

// verifyConcurrently 让 n 个 goroutine 同时起跑调用 verify(i)，返回成功次数。
func verifyConcurrently(n int, verify func(i int) bool) int32 {
	var ok atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if verify(i) {
				ok.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	return ok.Load()
}

func BenchmarkGenerateString(b *testing.B) {
	cp := New(String, memory.New())
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := cp.Generate(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
