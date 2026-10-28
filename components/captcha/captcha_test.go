package captcha

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mojocn/base64Captcha"

	"github.com/apus-run/gala/components/cache/memory"
)

func TestDigitEndToEnd(t *testing.T) {
	SetStore(NewCacheStore(memory.New(), 10*time.Minute))

	id, _, answer, err := DriverDigitFunc()
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(id, answer, true) {
		t.Fatal("answer should verify")
	}
	if Verify(id, answer, false) {
		t.Fatal("cleared captcha must not verify twice")
	}
}

func TestStringGenerate(t *testing.T) {
	SetStore(NewCacheStore(memory.New(), 10*time.Minute))

	id, b64s, answer, err := DriverStringFunc()
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || b64s == "" || answer == "" {
		t.Fatalf("empty result: id=%q b64s=%q answer=%q", id, b64s, answer)
	}
	if !Verify(id, answer, true) {
		t.Fatal("answer should verify")
	}
}

func TestCacheStoreClear(t *testing.T) {
	s := NewCacheStore(memory.New(), time.Minute)

	if err := s.Set("id1", "1234"); err != nil {
		t.Fatal(err)
	}
	if got := s.Get("id1", false); got != "1234" {
		t.Fatalf("get = %q, want 1234", got)
	}
	if got := s.Get("id1", true); got != "1234" {
		t.Fatalf("get clear = %q, want 1234", got)
	}
	if got := s.Get("id1", false); got != "" {
		t.Fatalf("cleared = %q, want empty", got)
	}
}

// 空答案绕过：store 对不存在的 id 返回空串，空答案曾经能通过校验。
func TestVerifyRejectsEmpty(t *testing.T) {
	for name, s := range map[string]base64Captcha.Store{
		"cacheStore": NewCacheStore(memory.New(), time.Minute),
		"memStore":   base64Captcha.NewMemoryStore(100, time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			SetStore(s)
			for _, c := range []struct{ id, code string }{
				{"no-such-id", ""}, {"", ""}, {"", "1234"}, {"no-such-id", "   "},
			} {
				if Verify(c.id, c.code, true) {
					t.Errorf("Verify(%q, %q) = true, want false", c.id, c.code)
				}
			}
			if s.Verify("no-such-id", "", true) {
				t.Error("store.Verify with an empty answer must fail")
			}
		})
	}
}

// 字符串验证码答案是小写：用户输大写或带首尾空白也应通过，与 base64Captcha 自带 store 一致。
func TestVerifyIgnoresCaseAndSpace(t *testing.T) {
	SetStore(NewCacheStore(memory.New(), time.Minute))
	for _, variant := range []func(string) string{strings.ToUpper, func(s string) string { return " " + s + "\t" }} {
		id, _, ans, err := DriverStringFunc()
		if err != nil {
			t.Fatal(err)
		}
		if !Verify(id, variant(ans), true) {
			t.Fatalf("Verify(%q) should accept %q", ans, variant(ans))
		}
	}
}

// 答错也作废（clear=true），防止对同一个验证码暴力枚举。
func TestWrongAnswerClears(t *testing.T) {
	SetStore(NewCacheStore(memory.New(), time.Minute))
	id, _, ans, _ := DriverDigitFunc()
	if Verify(id, "wrong", true) {
		t.Fatal("wrong answer verified")
	}
	if Verify(id, ans, true) {
		t.Fatal("captcha must be spent after a wrong attempt")
	}
}

// 并发校验同一个验证码只能成功一次（单实例 memory 后端）。
func TestConcurrentVerifyOnlyOnce(t *testing.T) {
	SetStore(NewCacheStore(memory.New(), time.Minute))
	for range 50 {
		id, _, ans, _ := DriverDigitFunc()
		if n := verifyConcurrently(func(int) bool { return Verify(id, ans, true) }, 32); n != 1 {
			t.Fatalf("one captcha verified %d times, want 1", n)
		}
	}
}

// SetStore 与生成、校验并发调用：不能有数据竞争（配合 -race），也不再改写第三方全局变量。
func TestSetStoreConcurrentAndIsolated(t *testing.T) {
	before := base64Captcha.DefaultMemStore
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); SetStore(NewCacheStore(memory.New(), time.Minute)) }()
		go func() {
			defer wg.Done()
			id, _, ans, err := DriverDigitFunc()
			if err == nil {
				_ = Verify(id, ans, true)
			}
		}()
	}
	wg.Wait()
	if base64Captcha.DefaultMemStore != before {
		t.Fatal("SetStore must not overwrite base64Captcha.DefaultMemStore")
	}
}

// key 有命名空间：调用方传入的 id 只能命中验证码，删不掉共享 cache 里的其他 key。
func TestCacheStoreKeyNamespace(t *testing.T) {
	c := memory.New()
	ctx := context.Background()
	if err := c.Set(ctx, "session:42", "secret", time.Minute); err != nil {
		t.Fatal(err)
	}
	SetStore(NewCacheStore(c, time.Minute))

	if Verify("session:42", "secret", true) {
		t.Fatal("Verify must not read arbitrary cache keys")
	}
	if v, err := c.Get(ctx, "session:42"); err != nil || v != "secret" {
		t.Fatalf("unrelated key was touched: %v, %v", v, err)
	}

	id, _, _, err := DriverDigitFunc()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Contains(ctx, "captcha:"+id) {
		t.Fatal("captcha should be stored under the captcha: prefix")
	}
}

// expiration<=0 不能变成永不过期。
func TestCacheStoreDefaultExpiration(t *testing.T) {
	for _, exp := range []time.Duration{0, -time.Second} {
		s := NewCacheStore(memory.New(), exp).(*cacheStore)
		if s.expiration != base64Captcha.Expiration {
			t.Fatalf("NewCacheStore(%v).expiration = %v, want %v", exp, s.expiration, base64Captcha.Expiration)
		}
	}
	if s := NewCacheStore(memory.New(), time.Minute).(*cacheStore); s.expiration != time.Minute {
		t.Fatalf("explicit expiration overridden: %v", s.expiration)
	}
}

// verifyConcurrently 让 n 个 goroutine 同时起跑调用 verify(i)，返回成功次数。
func verifyConcurrently(verify func(i int) bool, n int) int32 {
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

// naiveStore 不做任何校验：模拟用户经 SetStore 装入的自定义 store。
// 包级 Verify 的空值拦截与去空白必须不依赖 store 自己做。
type naiveStore struct{ m sync.Map }

func (s *naiveStore) Set(id, v string) error { s.m.Store(id, v); return nil }
func (s *naiveStore) Get(id string, clear bool) string {
	v, _ := s.m.Load(id)
	if clear {
		s.m.Delete(id)
	}
	sv, _ := v.(string)
	return sv
}
func (s *naiveStore) Verify(id, answer string, clear bool) bool { return s.Get(id, clear) == answer }

func TestVerifyGuardsDoNotDependOnStore(t *testing.T) {
	SetStore(&naiveStore{})
	if Verify("no-such-id", "", true) {
		t.Fatal("empty answer must fail even with a store that does not check")
	}
	id, _, ans, err := DriverDigitFunc()
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(id, " "+ans+" ", true) {
		t.Fatal("surrounding spaces must be trimmed before reaching the store")
	}

	// 默认内存 store 不去空白，同样要通过。
	SetStore(base64Captcha.NewMemoryStore(100, time.Minute))
	id, _, ans, _ = DriverDigitFunc()
	if !Verify(id, "\t"+ans+" ", true) {
		t.Fatal("surrounding spaces must be trimmed for the default memory store")
	}
}

func BenchmarkDriverString(b *testing.B) {
	SetStore(NewCacheStore(memory.New(), time.Minute))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, err := DriverStringFunc(); err != nil {
			b.Fatal(err)
		}
	}
}
