package fileserver

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestUploadOptionsDefaults(t *testing.T) {
	uo, err := NewUploadOptions()
	if err != nil {
		t.Fatal(err)
	}
	if size, ok := uo.Size(); size != 0 || ok {
		t.Fatalf("Size() = (%d, %v), want (0, false) when undeclared", size, ok)
	}
	if uo.ContentType() != "" || uo.Metadata() != nil || uo.IfNotExists() || uo.Progress() != nil {
		t.Fatalf("defaults are not empty: %+v", uo)
	}

	var zero UploadOptions
	if size, ok := zero.Size(); size != 0 || ok || zero.Metadata() != nil {
		t.Fatalf("zero UploadOptions is not the empty configuration")
	}
}

func TestNilOptionsAreNoOps(t *testing.T) {
	if _, err := NewUploadOptions(nil, WithSize(3), nil); err != nil {
		t.Fatalf("NewUploadOptions with nil options: %v", err)
	}
	if _, err := NewDownloadOptions(nil); err != nil {
		t.Fatalf("NewDownloadOptions with nil option: %v", err)
	}
	if _, err := NewReadOptions(nil); err != nil {
		t.Fatalf("NewReadOptions with nil option: %v", err)
	}
}

func TestUploadOptionsSizeAndFlags(t *testing.T) {
	uo, err := NewUploadOptions(WithSize(0), WithContentType("text/plain"), WithIfNotExists())
	if err != nil {
		t.Fatal(err)
	}
	if size, ok := uo.Size(); size != 0 || !ok {
		t.Fatalf("Size() = (%d, %v), want declared zero", size, ok)
	}
	if uo.ContentType() != "text/plain" || !uo.IfNotExists() {
		t.Fatalf("ContentType or IfNotExists not applied: %+v", uo)
	}
}

func TestNegativeSizeIsInvalid(t *testing.T) {
	_, err := NewUploadOptions(WithSize(-1))
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("WithSize(-1) error = %v, want ErrInvalidArgument", err)
	}
}

func TestMetadataIsCopiedAndLowercased(t *testing.T) {
	src := map[string]string{"Color": "red"}
	opt := WithMetadata(src)
	src["Color"] = "mutated"
	src["extra"] = "x"

	uo, err := NewUploadOptions(opt)
	if err != nil {
		t.Fatal(err)
	}
	got := uo.Metadata()
	if len(got) != 1 || got["color"] != "red" {
		t.Fatalf("Metadata() = %v, want map[color:red] (copied at WithMetadata)", got)
	}
	got["color"] = "changed"
	if uo.Metadata()["color"] != "red" {
		t.Fatal("mutating the returned map changed the options")
	}
}

func TestMetadataCollisionAfterLowercasingIsInvalid(t *testing.T) {
	_, err := NewUploadOptions(WithMetadata(map[string]string{"Key": "a", "key": "b"}))
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("colliding metadata keys error = %v, want ErrInvalidArgument", err)
	}
}

func TestEmptyMetadataKeyIsInvalid(t *testing.T) {
	_, err := NewUploadOptions(WithMetadata(map[string]string{"": "v"}))
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty metadata key error = %v, want ErrInvalidArgument", err)
	}
}

func TestEmptyMetadataIsNil(t *testing.T) {
	uo, err := NewUploadOptions(WithMetadata(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if uo.Metadata() != nil {
		t.Fatalf("Metadata() = %v, want nil for an empty map", uo.Metadata())
	}
}

func TestContentTypeWithControlCharacterIsInvalid(t *testing.T) {
	_, err := NewUploadOptions(WithContentType("text/plain\r\nX-Injected: 1"))
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("content type with CRLF error = %v, want ErrInvalidArgument", err)
	}
}

func TestProgressIsSharedByUploadAndDownload(t *testing.T) {
	p := ProgressFunc(func(context.Context, ProgressInfo) {})

	uo, err := NewUploadOptions(WithProgress(p))
	if err != nil {
		t.Fatal(err)
	}
	if uo.Progress() == nil {
		t.Fatal("upload progress not applied")
	}
	do, err := NewDownloadOptions(WithProgress(p))
	if err != nil {
		t.Fatal(err)
	}
	if do.Progress() == nil {
		t.Fatal("download progress not applied")
	}
}

func TestRangeDefaultsAndValidation(t *testing.T) {
	do, err := NewDownloadOptions()
	if err != nil {
		t.Fatal(err)
	}
	if off, length := do.Range(); off != 0 || length != -1 {
		t.Fatalf("default download range = (%d, %d), want (0, -1)", off, length)
	}
	ro, err := NewReadOptions()
	if err != nil {
		t.Fatal(err)
	}
	if off, length := ro.Range(); off != 0 || length != -1 {
		t.Fatalf("default read range = (%d, %d), want (0, -1)", off, length)
	}

	var zeroRead ReadOptions
	if off, length := zeroRead.Range(); off != 0 || length != -1 {
		t.Fatalf("zero ReadOptions range = (%d, %d), want (0, -1)", off, length)
	}

	ranged, err := NewReadOptions(WithRange(5, 10))
	if err != nil {
		t.Fatal(err)
	}
	if off, length := ranged.Range(); off != 5 || length != 10 {
		t.Fatalf("read range = (%d, %d), want (5, 10)", off, length)
	}
}

func TestInvalidRangesAreRejected(t *testing.T) {
	cases := []RangeOpt{WithRange(-1, 2), WithRange(0, -2), WithRange(-5, -5)}
	for _, opt := range cases {
		if _, err := NewDownloadOptions(opt); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("NewDownloadOptions(%+v) error = %v, want ErrInvalidArgument", opt, err)
		}
		if _, err := NewReadOptions(opt); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("NewReadOptions(%+v) error = %v, want ErrInvalidArgument", opt, err)
		}
	}
}

func TestZeroLengthRangeIsValid(t *testing.T) {
	ro, err := NewReadOptions(WithRange(3, 0))
	if err != nil {
		t.Fatalf("WithRange(3, 0) rejected: %v", err)
	}
	if off, length := ro.Range(); off != 3 || length != 0 {
		t.Fatalf("range = (%d, %d), want (3, 0)", off, length)
	}
}

func TestZeroRangeOptionIsNoOp(t *testing.T) {
	var ro RangeOpt
	read, err := NewReadOptions(ro)
	if err != nil {
		t.Fatalf("zero RangeOpt rejected by Read: %v", err)
	}
	if off, length := read.Range(); off != 0 || length != -1 {
		t.Fatalf("zero RangeOpt read range = (%d, %d), want the whole object", off, length)
	}
	download, err := NewDownloadOptions(ro)
	if err != nil {
		t.Fatalf("zero RangeOpt rejected by Download: %v", err)
	}
	if off, length := download.Range(); off != 0 || length != -1 {
		t.Fatalf("zero RangeOpt download range = (%d, %d), want the whole object", off, length)
	}
}

func TestMaxInt64SizeIsInvalid(t *testing.T) {
	_, err := NewUploadOptions(WithSize(math.MaxInt64))
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("WithSize(MaxInt64) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := NewUploadOptions(WithSize(math.MaxInt64 - 1)); err != nil {
		t.Fatalf("WithSize(MaxInt64-1) rejected: %v", err)
	}
}
