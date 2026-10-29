package id

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCode(t *testing.T) {
	type args struct {
		id      uint64
		options []func(*CodeOptions)
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{name: "default", args: args{id: 1}, want: "VHB4JX86"},
		{
			name: "with-options",
			args: args{
				id: 1,
				options: []func(*CodeOptions){
					WithCodeChars([]rune{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9'}),
					WithCodeN1(9),
					WithCodeN2(3),
					WithCodeL(5),
					WithCodeSalt(56789),
				},
			},
			want: "80773",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewCode(tt.args.id, tt.args.options...))
		})
	}
}

func BenchmarkNewCode(b *testing.B) {
	for b.Loop() {
		NewCode(1)
	}
}

func BenchmarkNewCodeTimeConsuming(b *testing.B) {
	b.StopTimer() // 调用该函数停止压力测试的时间计数

	id := NewCode(1)
	assert.Equal(b, "VHB4JX86", id)

	b.StartTimer() // 重新开始时间

	for b.Loop() {
		NewCode(1)
	}
}

func TestCheckedCodeCompatibilityAndCapacity(t *testing.T) {
	for _, n := range []uint64{0, 1, 2, 1000, 656099999999} {
		got, err := NewCodeChecked(n)
		if err != nil || got != NewCode(n) {
			t.Fatalf("compatibility for %d: %s %v", n, got, err)
		}
	}
	if _, err := NewCodeChecked(656100000000); err == nil {
		t.Fatal("accepted overflowing ID")
	}
}

func TestCheckedCodeIsInjectiveAcrossSmallDomain(t *testing.T) {
	opts := []func(*CodeOptions){WithCodeChars([]rune("0123456789")), WithCodeL(3), WithCodeN1(7), WithCodeN2(2), WithCodeSalt(^uint64(0))}
	seen := map[string]bool{}
	for n := uint64(0); n < 1000; n++ {
		code, err := NewCodeChecked(n, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			t.Fatalf("duplicate at %d: %s", n, code)
		}
		seen[code] = true
	}
}

func TestCheckedCodeRejectsInvalidPermutations(t *testing.T) {
	for _, opts := range [][]func(*CodeOptions){
		{WithCodeChars([]rune("aa"))}, {WithCodeChars([]rune{'a', -1})}, {WithCodeN1(30)}, {WithCodeN2(2)}, {WithCodeL(256)},
	} {
		if _, err := NewCodeChecked(1, opts...); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestCheckedCodeRejectsOptionsIgnoredByLegacyAPI(t *testing.T) {
	cases := []struct {
		name   string
		option func(*CodeOptions)
	}{
		{"zero length", WithCodeL(0)},
		{"negative length", WithCodeL(-1)},
		{"nil alphabet", WithCodeChars(nil)},
		{"empty alphabet", WithCodeChars([]rune{})},
	}
	custom := []func(*CodeOptions){
		WithCodeChars([]rune("0123456789")), WithCodeL(5),
		WithCodeN1(9), WithCodeN2(3), WithCodeSalt(56789),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, base := range [][]func(*CodeOptions){nil, custom} {
				opts := append(append([]func(*CodeOptions){}, base...), tc.option)
				if code, err := NewCodeChecked(1, opts...); err == nil || code != "" {
					t.Fatalf("invalid option produced code=%q err=%v", code, err)
				}
				// Reuse the same option closures after the checked call: legacy
				// behavior must still ignore invalid values, including overrides.
				if got, want := NewCode(1, opts...), NewCode(1, base...); got != want {
					t.Fatalf("legacy behavior changed: got %q want %q", got, want)
				}
			}
		})
	}
}

func TestCheckedCodeUsesLastOptionValue(t *testing.T) {
	for _, opts := range [][]func(*CodeOptions){
		{WithCodeL(0), WithCodeL(8)},
		{WithCodeChars(nil), WithCodeChars([]rune("0123456789"))},
	} {
		code, err := NewCodeChecked(1, opts...)
		if err != nil || code != NewCode(1, opts...) {
			t.Fatalf("valid final configuration: code=%q err=%v", code, err)
		}
	}
}

// 超出 byte 范围的字符表或长度不能让旧实现除零 panic。
func TestNewCodeDoesNotPanicOnOutOfRangeOptions(t *testing.T) {
	big := make([]rune, 256)
	for i := range big {
		big[i] = rune('a' + i%26 + (i/26)*100)
	}
	for name, opts := range map[string][]func(*CodeOptions){
		"alphabet of 256": {WithCodeChars(big)},
		"length 256":      {WithCodeL(256)},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("NewCode panicked: %v", r)
				}
			}()
			_ = NewCode(42, opts...)
		})
	}
	// 正常参数下的输出不变
	if got := NewCode(1); got == "" {
		t.Fatal("default options must still produce a code")
	}
}
