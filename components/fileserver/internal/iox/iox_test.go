package iox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

// scriptedReader 按脚本依次返回结果，脚本用完后返回 io.EOF。
type scriptedReader struct {
	steps []readStep
	calls int
}

type readStep struct {
	n   int
	err error
}

func (r *scriptedReader) Read(p []byte) (int, error) {
	r.calls++
	if len(r.steps) == 0 {
		return 0, io.EOF
	}
	s := r.steps[0]
	r.steps = r.steps[1:]
	return s.n, s.err
}

func TestReadSomeAbsorbsEmptyReads(t *testing.T) {
	r := &scriptedReader{steps: []readStep{{0, nil}, {0, nil}, {3, nil}}}
	n, err := ReadSome(r, make([]byte, 8))
	if n != 3 || err != nil || r.calls != 3 {
		t.Fatalf("ReadSome = %d, %v after %d reads; want 3, nil after 3", n, err, r.calls)
	}
}

func TestReadSomeReturnsDataTogetherWithError(t *testing.T) {
	r := &scriptedReader{steps: []readStep{{3, io.EOF}}}
	n, err := ReadSome(r, make([]byte, 8))
	if n != 3 || err != io.EOF {
		t.Fatalf("ReadSome = %d, %v; want 3, EOF", n, err)
	}
}

func TestReadSomePassesErrorsThrough(t *testing.T) {
	boom := errors.New("boom")
	r := &scriptedReader{steps: []readStep{{0, boom}}}
	if _, err := ReadSome(r, make([]byte, 8)); !errors.Is(err, boom) {
		t.Fatalf("ReadSome error = %v, want boom", err)
	}
}

func TestReadSomeReportsStuckReader(t *testing.T) {
	r := &scriptedReader{steps: make([]readStep, maxEmptyReads+10)}
	n, err := ReadSome(r, make([]byte, 8))
	if n != 0 || !errors.Is(err, io.ErrNoProgress) || r.calls != maxEmptyReads {
		t.Fatalf("ReadSome = %d, %v after %d reads; want 0, ErrNoProgress after %d", n, err, r.calls, maxEmptyReads)
	}
}

func TestResolveRange(t *testing.T) {
	cases := []struct {
		size, off, length int64
		start, n          int64
		wantErr           bool
	}{
		{10, 0, -1, 0, 10, false},
		{10, 2, 3, 2, 3, false},
		{10, 8, -1, 8, 2, false},
		{10, 8, 100, 8, 2, false},
		{10, 10, -1, 10, 0, false},
		{10, 11, -1, 0, 0, true},
		{0, 0, -1, 0, 0, false},
		{10, 0, 0, 0, 0, false},
	}
	for _, tc := range cases {
		start, n, err := ResolveRange(tc.size, tc.off, tc.length)
		if (err != nil) != tc.wantErr || (!tc.wantErr && (start != tc.start || n != tc.n)) {
			t.Errorf("ResolveRange(%d, %d, %d) = %d, %d, %v; want %d, %d, err=%v",
				tc.size, tc.off, tc.length, start, n, err, tc.start, tc.n, tc.wantErr)
		}
	}
}

// FuzzResolveRange 检查范围解析的不变量：结果始终落在对象之内，且大整数不会造成越界。
func FuzzResolveRange(f *testing.F) {
	f.Add(int64(10), int64(0), int64(-1))
	f.Add(int64(10), int64(9), int64(math.MaxInt64))
	f.Add(int64(0), int64(0), int64(0))
	f.Fuzz(func(t *testing.T, size, off, length int64) {
		if size < 0 || off < 0 || length < -1 {
			t.Skip("the options layer rejects these before ResolveRange runs")
		}
		start, n, err := ResolveRange(size, off, length)
		if err != nil {
			if off <= size {
				t.Fatalf("ResolveRange(%d, %d, %d) failed although the offset is inside the object", size, off, length)
			}
			return
		}
		if start != off || n < 0 || start+n > size {
			t.Fatalf("ResolveRange(%d, %d, %d) = %d, %d escapes the object", size, off, length, start, n)
		}
		if length >= 0 && n > length {
			t.Fatalf("ResolveRange(%d, %d, %d) = %d bytes, want at most %d", size, off, length, n, length)
		}
	})
}

func TestCopyNStopsAtN(t *testing.T) {
	var dst bytes.Buffer
	n, err := CopyN(context.Background(), &dst, strings.NewReader("hello world"), 5, make([]byte, 2))
	if n != 5 || err != nil || dst.String() != "hello" {
		t.Fatalf("CopyN = %d, %v, %q; want 5, nil, hello", n, err, dst.String())
	}
}

func TestCopyNReportsShortSourceWithoutError(t *testing.T) {
	var dst bytes.Buffer
	n, err := CopyN(context.Background(), &dst, strings.NewReader("ab"), 5, make([]byte, 2))
	if n != 2 || err != nil {
		t.Fatalf("CopyN = %d, %v; want 2, nil (the caller decides whether a short copy is an error)", n, err)
	}
}

func TestCopyNHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dst bytes.Buffer
	_, err := CopyN(ctx, &dst, strings.NewReader("hello"), 5, make([]byte, 2))
	if !errors.Is(err, context.Canceled) || dst.Len() != 0 {
		t.Fatalf("CopyN with canceled context = %v, wrote %q; want context.Canceled and no bytes", err, dst.String())
	}
}

func TestReaderChecksContextBeforeEachRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := Reader(ctx, strings.NewReader("abc"))
	buf := make([]byte, 1)
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("first read before cancel: %v", err)
	}
	cancel()
	if _, err := r.Read(buf); !errors.Is(err, context.Canceled) {
		t.Fatalf("read after cancel = %v, want context.Canceled", err)
	}
}
