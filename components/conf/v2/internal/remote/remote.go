// Package remote contains shared rules for KV source adapters.
package remote

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"
)

// Directory returns a prefix ending in a slash. Empty signals invalid input.
// The caller handles the SDK's leading slash semantics.
func Directory(path string) string {
	if path == "" || strings.Contains(path, "//") {
		return ""
	}
	if strings.HasSuffix(path, "/") {
		return path
	}
	return path + "/"
}

// Nil reports whether a borrowed interface contains a nil client.
func Nil(client any) bool {
	if client == nil {
		return true
	}
	v := reflect.ValueOf(client)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// ReadError preserves cancellation while hiding SDK response details.
func ReadError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return errors.New("read failed")
	}
}

// Signal coalesces invalidations into the caller's buffered channel.
func Signal(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Retry waits before another watch request, interruptibly.
func Retry(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Second):
		return ctx.Err() == nil
	}
}
