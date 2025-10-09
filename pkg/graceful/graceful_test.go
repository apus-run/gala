package graceful

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestGraceful_Await_ContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gr := NewGraceful(ctx, cancel)

	gr.Add(1)
	go func() {
		defer gr.Done()
		time.Sleep(100 * time.Millisecond)
	}()

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := gr.Await()
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestGraceful_Await_Signal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gr := NewGraceful(ctx, cancel)

	gr.Add(1)
	go func() {
		defer gr.Done()
		time.Sleep(100 * time.Millisecond)
	}()

	// Simulate signal
	go func() {
		time.Sleep(50 * time.Millisecond)
		gr.osSigsCh <- syscall.SIGINT
	}()

	err := gr.Await()
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestGraceful_Timeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gr := NewGraceful(ctx, cancel)
	gr.SetGracefulTimeout(100 * time.Millisecond)

	gr.Add(1)
	// Never call Done, so WaitGroup never finishes

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := gr.Await()
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got %v", err)
	}
}

func TestGraceful_AddDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gr := NewGraceful(ctx, cancel)

	gr.Add(2)
	doneCh := make(chan struct{})
	go func() {
		gr.Done()
		gr.Done()
		close(doneCh)
	}()

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	select {
	case <-doneCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Done() did not complete in time")
	}

	err := gr.Await()
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestGraceful_SetGracefulTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gr := NewGraceful(ctx, cancel)
	gr.SetGracefulTimeout(123 * time.Millisecond)
	if gr.timeout != 123*time.Millisecond {
		t.Errorf("expected timeout to be set, got %v", gr.timeout)
	}
}
