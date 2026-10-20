package filestore

import (
	"bytes"
	"context"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

// BenchmarkUploadSmallDeclaredSize 测量声明长度的小对象上传。拷贝缓冲应随对象大小收缩。
func BenchmarkUploadSmallDeclaredSize(b *testing.B) {
	s, err := New(b.TempDir(), WithDurability(SyncNone))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	payload := bytes.Repeat([]byte("x"), 4096)
	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for range b.N {
		if err := s.Upload(ctx, "obj", bytes.NewReader(payload), fileserver.WithSize(int64(len(payload)))); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDownloadSmall 测量小对象的下载。
func BenchmarkDownloadSmall(b *testing.B) {
	s, err := New(b.TempDir(), WithDurability(SyncNone))
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	if err := s.Upload(ctx, "obj", bytes.NewReader(bytes.Repeat([]byte("x"), 4096))); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(4096)
	for range b.N {
		if err := s.Download(ctx, "obj", &discardAt{}); err != nil {
			b.Fatal(err)
		}
	}
}
