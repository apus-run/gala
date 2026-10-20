package fileserver_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/filestore"
)

func Example() {
	dir, err := os.MkdirTemp("", "fileserver-example")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	store, err := filestore.New(dir)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	var transferred int64
	err = store.Upload(ctx, "docs/hello", strings.NewReader("hello"),
		fileserver.WithIfNotExists(),
		fileserver.WithProgress(fileserver.ProgressFunc(func(_ context.Context, p fileserver.ProgressInfo) {
			transferred = p.Transferred
		})),
	)
	if err != nil {
		panic(err)
	}
	err = store.Upload(ctx, "docs/hello", strings.NewReader("overwrite"), fileserver.WithIfNotExists())
	if !errors.Is(err, fileserver.ErrConflict) {
		panic(err)
	}

	rc, err := store.Read(ctx, "docs/hello", fileserver.WithRange(1, 3))
	if err != nil {
		panic(err)
	}
	buf, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %d conflict\n", buf, transferred)
	// Output:
	// ell 5 conflict
}

// countingUploader 是调用方装饰器：库不内置指标，需要观察时包装自己用到的接口。
type countingUploader struct {
	fileserver.Uploader
	n int
}

func (c *countingUploader) Upload(ctx context.Context, key string, r io.Reader, opts ...fileserver.UploadOpt) error {
	c.n++
	return c.Uploader.Upload(ctx, key, r, opts...)
}

func Example_decorator() {
	dir, err := os.MkdirTemp("", "fileserver-example")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	store, err := filestore.New(dir)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	wrapped := &countingUploader{Uploader: store}
	if err := wrapped.Upload(context.Background(), "a", strings.NewReader("x")); err != nil {
		panic(err)
	}
	fmt.Println(wrapped.n)
	// Output:
	// 1
}
