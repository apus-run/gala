package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	out, err := captureStdout(t, func() error { return run(context.Background()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"csrf: cross-site post without token refused",
		"api: header client disabled user without cookies",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = orig
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}
