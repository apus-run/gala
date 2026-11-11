package conf_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	conf "github.com/apus-run/gala/components/conf/v2"
)

// Tests using validationBarrier are serial; the channels synchronize all access.
var validationBarrier struct{ entered, release chan struct{} }

type barrierConfig struct {
	Port int `conf:"port"`
}

func (c *barrierConfig) Validate() error {
	if c.Port == 2 {
		close(validationBarrier.entered)
		<-validationBarrier.release
	}
	return nil
}

func TestCancelBeforeValidationReturns(t *testing.T) {
	validationBarrier.entered = make(chan struct{})
	validationBarrier.release = make(chan struct{})
	source := &fakeSource{data: map[string]any{"port": 1}}
	c, err := conf.Open(t.Context(), barrierConfig{}, source)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Current()
	source.data["port"] = 2
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Reload(ctx) }()
	<-validationBarrier.entered
	cancel()
	close(validationBarrier.release)
	err = <-done
	assertFailure(t, err, conf.StageCanceled)
	if !errors.Is(err, context.Canceled) || c.Current() != old {
		t.Fatal("canceled candidate published")
	}
}

func TestCancelQueuedReloadWithoutReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		close(gate)
		source := &blockingSource{data: portFile(8080), gate: gate, started: make(chan struct{}, 8)}
		c, err := conf.Open(t.Context(), appConfig{}, source)
		if err != nil {
			t.Fatal(err)
		}
		<-source.started
		old := c.Current()
		source.data = portFile(9090)
		source.setGate(make(chan struct{}))
		first := make(chan error, 1)
		go func() { first <- c.Reload(t.Context()) }()
		<-source.started
		before := source.count()
		ctx, cancel := context.WithCancel(t.Context())
		queued := make(chan error, 1)
		go func() { queued <- c.Reload(ctx) }()
		synctest.Wait() // waiter is durably blocked behind the active reload
		cancel()
		err = <-queued
		assertFailure(t, err, conf.StageCanceled)
		if !errors.Is(err, context.Canceled) || source.count() != before {
			t.Fatal("canceled waiter called Load")
		}
		source.releaseGate()
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if c.Current().Server.Port != 9090 || old.Server.Port != 8080 {
			t.Fatal("only the successful call may publish")
		}
	})
}

func TestCancellationPublicationRace(t *testing.T) {
	for range 100 {
		source := &fakeSource{data: map[string]any{"port": 1}}
		c, err := conf.Open(t.Context(), typed{}, source)
		if err != nil {
			t.Fatal(err)
		}
		old := c.Current()
		source.data["port"] = 2
		ctx, cancel := context.WithCancel(t.Context())
		start := make(chan struct{})
		done := make(chan error, 1)
		canceled := make(chan struct{})
		go func() { <-start; done <- c.Reload(ctx) }()
		go func() { <-start; cancel(); close(canceled) }()
		close(start)
		err = <-done
		<-canceled
		if err == nil {
			if c.Current() == old || c.Current().Port != 2 {
				t.Fatal("success without publication")
			}
		} else {
			assertFailure(t, err, conf.StageCanceled)
			if c.Current() != old {
				t.Fatal("failure with publication")
			}
		}
	}
}

func TestCanceledLoadWinsOverReturnedError(t *testing.T) {
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		source := &fakeSource{data: map[string]any{"port": 1}}
		next := &fakeSource{}
		c, err := conf.Open(t.Context(), typed{}, source, next)
		if err != nil {
			t.Fatal(err)
		}
		old, calls := c.Current(), next.calls
		source.err = fmt.Errorf("SECRET: %w", sentinel)
		err = c.Reload(t.Context())
		assertFailure(t, err, conf.StageCanceled)
		if !errors.Is(err, sentinel) || c.Current() != old || next.calls != calls {
			t.Fatal("cancellation contract")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	source := conf.SourceFunc(func(context.Context) (map[string]any, error) {
		cancel()
		return nil, errors.New("SECRET")
	})
	_, err := conf.Open(ctx, typed{}, source)
	assertFailure(t, err, conf.StageCanceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("ctx did not take precedence")
	}
}

func TestCancellationStopsFollowingSourceAndPublication(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first := conf.SourceFunc(func(context.Context) (map[string]any, error) { cancel(); return map[string]any{}, nil })
	next := &fakeSource{}
	c, err := conf.Open(ctx, typed{}, first, next)
	if c != nil || !errors.Is(err, context.Canceled) || next.calls != 0 {
		t.Fatalf("cancellation: %v, %v, calls=%d", c, err, next.calls)
	}
}
