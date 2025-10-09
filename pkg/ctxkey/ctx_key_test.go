package ctxkey

import (
	"context"
	"testing"
)

type Info struct {
	ID   int64
	Name string
}

var infoKey = NewContextKey[*Info]()

func Name(ctx context.Context) string {
	info, ok := infoKey.FromContext(ctx)
	if !ok || info == nil {
		return ""
	}
	return info.Name
}

func WithInfo(ctx context.Context, id int64, name string) context.Context {
	return infoKey.NewContext(ctx, &Info{
		ID:   id,
		Name: name,
	})
}

func TestCtxCache(t *testing.T) {
	ctx := context.Background()
	ctx = WithInfo(ctx, 1, "test")

	if Name(ctx) != "test" {
		t.Fatalf("expect test, got %s", Name(ctx))
	}
	t.Logf("info: %+v", Name(ctx))
}
