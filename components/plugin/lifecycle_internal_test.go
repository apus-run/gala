package plugin

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"testing/synctest"
)

func TestProtectGoexitError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		err := protect(context.Background(), 0, func(context.Context) error {
			runtime.Goexit()
			return nil
		})
		if !errors.Is(err, errHookGoexit) {
			t.Fatalf("err = %v, want errHookGoexit", err)
		}
	})
}
