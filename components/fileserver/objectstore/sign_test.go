package objectstore

import (
	"errors"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
)

func TestSignOptionsDefaultsAndBounds(t *testing.T) {
	got, err := NewSignOptions()
	if err != nil {
		t.Fatal(err)
	}
	if got.Expiry() != DefaultExpiry {
		t.Fatalf("Expiry = %s, want %s", got.Expiry(), DefaultExpiry)
	}
	if ct := got.ContentType(); ct != "" {
		t.Fatalf("ContentType = %q", ct)
	}
	if n, ok := got.ContentLength(); n != 0 || ok {
		t.Fatalf("ContentLength = (%d, %v)", n, ok)
	}
	if name, ok := got.Disposition(); name != "" || ok {
		t.Fatalf("Disposition = (%q, %v)", name, ok)
	}

	got, err = NewSignOptions(WithExpiry(MinExpiry), WithContentType("text/plain"), WithContentLength(0), WithDisposition("a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Expiry() != MinExpiry || got.ContentType() != "text/plain" {
		t.Fatalf("options = %+v", got)
	}
	n, ok := got.ContentLength()
	if n != 0 || !ok {
		t.Fatalf("ContentLength = (%d, %v), want declared zero", n, ok)
	}
	name, ok := got.Disposition()
	if name != "a.txt" || !ok {
		t.Fatalf("Disposition = (%q, %v)", name, ok)
	}

	if _, err := NewSignOptions(nil, WithExpiry(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, d := range []time.Duration{0, MinExpiry - time.Nanosecond, MaxExpiry + time.Nanosecond} {
		_, err := NewSignOptions(WithExpiry(d))
		if !errors.Is(err, fileserver.ErrInvalidArgument) {
			t.Errorf("expiry %s: error = %v, want ErrInvalidArgument", d, err)
		}
	}
	if _, err := NewSignOptions(WithContentLength(-1)); !errors.Is(err, fileserver.ErrInvalidArgument) {
		t.Fatalf("negative length error = %v", err)
	}
	if _, err := NewSignOptions(WithExpiry(MaxExpiry)); err != nil {
		t.Fatalf("max expiry: %v", err)
	}
}
