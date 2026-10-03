package client

import (
	"testing"
	"time"
)

func TestNewRejectsInvalidFinalConfig(t *testing.T) {
	for _, opts := range [][]Option{
		{WithTimeout(0)},
		{WithRetries(-1)},
	} {
		c, err := New(opts...)
		if err == nil || c != nil {
			t.Fatalf("New returned client=%v error=%v for invalid config", c, err)
		}
	}
}

func TestNewUsesFinalState(t *testing.T) {
	c, err := New(WithTimeout(-time.Second), WithTimeout(time.Second), WithRetries(0))
	if err != nil {
		t.Fatalf("valid final state rejected: %v", err)
	}
	if c.options.timeout != time.Second || c.options.retries != 0 {
		t.Fatalf("unexpected final state: %+v", c.options)
	}
}

func TestClientOptionsReturnsDetachedCopy(t *testing.T) {
	c, err := New(WithLabels(map[string]string{"service": "orders"}))
	if err != nil {
		t.Fatal(err)
	}
	first := c.Options()
	first.Apply(WithTimeout(time.Second))
	first.labels["service"] = "changed"
	second := c.Options()
	if second == first || second.timeout != 5*time.Second || second.labels["service"] != "orders" {
		t.Fatalf("runtime state leaked: %+v", second)
	}
}

func TestNewDetachesCapturedConstructionPointer(t *testing.T) {
	var captured *Options
	c, err := New(
		WithLabels(map[string]string{"service": "orders"}),
		OptionFunc(func(o *Options) { captured = o }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil {
		t.Fatal("test did not capture configuration")
	}
	captured.Apply(WithTimeout(time.Second))
	captured.labels["service"] = "changed"
	if c.options.timeout != 5*time.Second || c.options.labels["service"] != "orders" {
		t.Fatalf("client shares construction state: %+v", c.options)
	}
}
