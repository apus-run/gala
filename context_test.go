package gala

import (
	"context"
	"testing"
)

func TestRequestMetadataKeyRoundTrip(t *testing.T) {
	ctx := RequestMetadataKey.NewContext(context.Background(), RequestMetadata{HeaderTraceID: "t-1"})
	md, ok := RequestMetadataKey.FromContext(ctx)
	if !ok || md[HeaderTraceID] != "t-1" {
		t.Fatalf("round-trip failed: ok=%v md=%v", ok, md)
	}
	if _, ok := RequestMetadataKey.FromContext(context.Background()); ok {
		t.Fatal("expected no metadata on a bare context")
	}
}

func TestWithRequestMetadataDeepCopies(t *testing.T) {
	src := RequestMetadata{HeaderUserID: "u-1"}
	ctx := WithRequestMetadata(context.Background(), src)
	src[HeaderUserID] = "mutated" // caller mutation must not leak in

	if got := GetRequestMetadata(ctx, HeaderUserID); got != "u-1" {
		t.Fatalf("got %q, want u-1 (context value was not deep-copied)", got)
	}
}

func TestGetRequestMetadata(t *testing.T) {
	ctx := WithRequestMetadata(context.Background(), RequestMetadata{HeaderUserID: "u-1"})
	if got := GetRequestMetadata(ctx, HeaderUserID); got != "u-1" {
		t.Fatalf("got %q, want u-1", got)
	}
	if got := GetRequestMetadata(ctx, "missing"); got != "" {
		t.Fatalf("got %q, want empty for missing key", got)
	}
	if got := GetRequestMetadata(context.Background(), HeaderUserID); got != "" {
		t.Fatalf("got %q, want empty when no metadata present", got)
	}
}

func TestWithRequestMetadataValueIsCopyOnWrite(t *testing.T) {
	base := WithRequestMetadata(context.Background(), RequestMetadata{HeaderTraceID: "t-1"})
	derived := WithRequestMetadataValue(base, HeaderUserID, "u-1")

	// base must be untouched.
	if _, ok := mustMD(t, base)[HeaderUserID]; ok {
		t.Fatal("WithRequestMetadataValue mutated the parent context")
	}
	// derived must carry both keys.
	d := mustMD(t, derived)
	if d[HeaderTraceID] != "t-1" || d[HeaderUserID] != "u-1" {
		t.Fatalf("derived metadata wrong: %v", d)
	}

	// works even when ctx had no metadata at all.
	fresh := WithRequestMetadataValue(context.Background(), HeaderColorTag, "gray")
	if got := GetRequestMetadata(fresh, HeaderColorTag); got != "gray" {
		t.Fatalf("got %q, want gray", got)
	}
}

func TestWithRequestMetadataMerges(t *testing.T) {
	base := WithRequestMetadata(context.Background(), RequestMetadata{HeaderTraceID: "t-1"})
	merged := WithRequestMetadata(base, RequestMetadata{HeaderTraceID: "t-2", HeaderUserID: "u-1"})

	m := mustMD(t, merged)
	if m[HeaderTraceID] != "t-2" || m[HeaderUserID] != "u-1" {
		t.Fatalf("merge wrong: %v", m)
	}
	if mustMD(t, base)[HeaderTraceID] != "t-1" {
		t.Fatal("WithRequestMetadata mutated the parent context")
	}
	if WithRequestMetadata(base, nil) != base {
		t.Fatal("WithRequestMetadata(empty) should return ctx unchanged")
	}
}

func TestWithoutRequestMetadata(t *testing.T) {
	base := WithRequestMetadata(context.Background(), RequestMetadata{
		HeaderTraceID: "t-1",
		HeaderUserID:  "u-1",
	})
	stripped := WithoutRequestMetadata(base, HeaderUserID)

	if got := GetRequestMetadata(stripped, HeaderUserID); got != "" {
		t.Fatalf("got %q, want key removed", got)
	}
	if got := GetRequestMetadata(stripped, HeaderTraceID); got != "t-1" {
		t.Fatalf("got %q, want t-1 retained", got)
	}
	if got := GetRequestMetadata(base, HeaderUserID); got != "u-1" {
		t.Fatal("WithoutRequestMetadata mutated the parent context")
	}
	if WithoutRequestMetadata(base) != base {
		t.Fatal("WithoutRequestMetadata() with no keys should return ctx unchanged")
	}
}

func mustMD(t *testing.T, ctx context.Context) RequestMetadata {
	t.Helper()
	md, ok := RequestMetadataKey.FromContext(ctx)
	if !ok {
		t.Fatal("expected metadata on context")
	}
	return md
}
