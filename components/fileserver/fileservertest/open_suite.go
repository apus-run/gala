package fileservertest

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

// RunOpenSuite 验证可选 Opener 的范围、属性和打开后版本稳定性。工厂必须返回实现 Opener 的 Storage。
func RunOpenSuite(t *testing.T, factory StorageFactory) {
	t.Helper()
	t.Run("open keeps content and attributes on the opened version", func(t *testing.T) {
		s := factory(t)
		mustPut(t, s, "k", []byte("old-version"))
		opener, ok := s.(fileserver.Opener)
		if !ok {
			t.Fatal("storage does not implement Opener")
		}
		r, err := opener.Open(t.Context(), "k")
		requireNoErr(t, err, "Open")
		t.Cleanup(func() { _ = r.Body.Close() })
		mustPut(t, s, "k", []byte("new"))
		requireNoErr(t, s.Remove(t.Context(), "k"), "Remove")
		data, err := io.ReadAll(r.Body)
		requireNoErr(t, err, "Read")
		if string(data) != "old-version" || r.Info.Key != "k" || r.Info.Size != 11 || r.Offset != 0 || r.Length != 11 {
			t.Fatalf("opened view changed: result=%+v body=%q", r, data)
		}
	})
	t.Run("open reports total size separately from range length", func(t *testing.T) {
		s := factory(t)
		mustPut(t, s, "k", []byte("0123456789"))
		opener := s.(fileserver.Opener)
		for _, tc := range []struct {
			off, length int64
			want        string
		}{
			{2, 3, "234"}, {8, 100, "89"}, {10, -1, ""}, {3, 0, ""},
		} {
			r, err := opener.Open(t.Context(), "k", fileserver.WithRange(tc.off, tc.length))
			requireNoErr(t, err, "Open")
			data, err := io.ReadAll(r.Body)
			requireNoErr(t, err, "Read")
			requireNoErr(t, r.Body.Close(), "Close")
			if string(data) != tc.want || r.Info.Size != 10 || r.Offset != tc.off || r.Length != int64(len(tc.want)) {
				t.Fatalf("range (%d,%d): result=%+v body=%q", tc.off, tc.length, r, data)
			}
		}
		if _, err := opener.Open(t.Context(), "k", fileserver.WithRange(11, 1)); !errors.Is(err, fileserver.ErrInvalidArgument) {
			t.Fatalf("out of bounds error = %v", err)
		}
		if _, err := opener.Open(t.Context(), "missing"); !errors.Is(err, fileserver.ErrNotFound) {
			t.Fatalf("missing error = %v", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := opener.Open(ctx, "k"); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled error = %v", err)
		}
	})
	t.Run("empty object is a valid view", func(t *testing.T) {
		s := factory(t)
		requireNoErr(t, s.Upload(t.Context(), "empty", strings.NewReader("")), "Upload")
		r, err := s.(fileserver.Opener).Open(t.Context(), "empty")
		requireNoErr(t, err, "Open")
		defer r.Body.Close()
		data, err := io.ReadAll(r.Body)
		requireNoErr(t, err, "Read")
		if len(data) != 0 || r.Info.Size != 0 || r.Length != 0 || r.Offset != 0 {
			t.Fatalf("empty result=%+v body=%q", r, data)
		}
	})
}
