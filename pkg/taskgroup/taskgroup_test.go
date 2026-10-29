package taskgroup

import (
	"context"
	"testing"
	"time"
)

func TestPanicIsReportedAsError(t *testing.T) {
	tg := NewTaskGroup(context.Background(), 2)
	tg.Go(func() error { panic("boom") })
	if err := tg.Wait(); err == nil {
		t.Fatal("Wait returned nil although a task panicked")
	}
}

func TestZeroLimitDoesNotBlock(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		tg := NewUninterruptibleTaskGroup(context.Background(), 0)
		tg.Go(func() error { return nil })
		done <- tg.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Go blocked forever with limit 0")
	}
}
