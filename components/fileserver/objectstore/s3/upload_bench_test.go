package s3

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

// Small uploads must not pay for unused multipart workers.
func BenchmarkUploadSmallConcurrency(b *testing.B) {
	for _, concurrency := range []int{1, 8, 32} {
		for _, declared := range []bool{false, true} {
			b.Run(fmt.Sprintf("workers=%d/declared=%t", concurrency, declared), func(b *testing.B) {
				s, err := New(newMemAPI(), Config{Bucket: "b", Concurrency: concurrency})
				if err != nil {
					b.Fatal(err)
				}
				data := bytes.Repeat([]byte("x"), 4096)
				var opts []fileserver.UploadOpt
				if declared {
					opts = append(opts, fileserver.WithSize(int64(len(data))))
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				b.ResetTimer()
				for range b.N {
					if err := s.Upload(b.Context(), "k", bytes.NewReader(data), opts...); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
