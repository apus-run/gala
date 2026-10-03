package client

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestDefaultOptionsIndependent(t *testing.T) {
	a, b := DefaultOptions(), DefaultOptions()
	if a == b {
		t.Fatal("default options share a pointer")
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("defaults are invalid: %v", err)
	}
	a.timeout = time.Second
	a.labels["service"] = "a"
	if b.timeout != 5*time.Second || b.retries != 3 || len(b.labels) != 0 {
		t.Fatalf("default instances are not independent: %+v", b)
	}
}

func TestNewOptionsAppliesInOrder(t *testing.T) {
	var seen []time.Duration
	observe := OptionFunc(func(o *Options) { seen = append(seen, o.timeout) })
	o := NewOptions(
		observe,
		WithTimeout(2*time.Second),
		observe,
		WithTimeout(7*time.Second),
		observe,
	)
	want := []time.Duration{5 * time.Second, 2 * time.Second, 7 * time.Second}
	if !reflect.DeepEqual(seen, want) || o.timeout != 7*time.Second {
		t.Fatalf("application order: seen=%v, timeout=%v", seen, o.timeout)
	}
}

func TestApplyDoesNotReset(t *testing.T) {
	o := NewOptions(WithTimeout(time.Second), WithLabels(map[string]string{"a": "b"}))
	o.Apply(WithRetries(0))
	o.Apply()
	if o.timeout != time.Second || o.retries != 0 || o.labels["a"] != "b" {
		t.Fatalf("Apply reset unrelated configuration: %+v", o)
	}
}

func TestExplicitZeroIsPreserved(t *testing.T) {
	o := NewOptions(WithRetries(0))
	if o.retries != 0 {
		t.Fatalf("explicit zero overwritten: %d", o.retries)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("zero retries must be valid: %v", err)
	}
}

func TestNewOptionsDoesNotValidate(t *testing.T) {
	o := NewOptions(WithTimeout(-time.Second), WithRetries(-1))
	if o.timeout != -time.Second || o.retries != -1 {
		t.Fatalf("configuration was normalized: %+v", o)
	}
	if err := o.Validate(); err == nil {
		t.Fatal("invalid configuration was accepted by Validate")
	}
	o.Apply(WithTimeout(time.Second), WithRetries(0))
	if err := o.Validate(); err != nil {
		t.Fatalf("valid final state was rejected: %v", err)
	}
}

func TestValidateDoesNotMutate(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr bool
	}{
		{name: "defaults"},
		{name: "zero timeout", opts: []Option{WithTimeout(0)}, wantErr: true},
		{name: "negative timeout", opts: []Option{WithTimeout(-time.Second)}, wantErr: true},
		{name: "negative retries", opts: []Option{WithRetries(-1)}, wantErr: true},
		{name: "zero retries", opts: []Option{WithRetries(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := NewOptions(tt.opts...)
			before := o.clone()
			err := o.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v, wantErr=%v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(*o, before) {
				t.Fatal("Validate mutated configuration")
			}
		})
	}
}

func TestWithLabelsSnapshotsInput(t *testing.T) {
	labels := map[string]string{"service": "orders"}
	opt := WithLabels(labels)
	labels["service"] = "changed"
	labels["extra"] = "outside"
	o := NewOptions(opt)
	if o.labels["service"] != "orders" || len(o.labels) != 1 {
		t.Fatalf("Option shares caller input: %v", o.labels)
	}
}

func TestWithLabelsReuseOwnsContainers(t *testing.T) {
	opt := WithLabels(map[string]string{"service": "orders"})
	a, b := NewOptions(opt), NewOptions(opt)
	a.labels["service"] = "changed"
	if b.labels["service"] != "orders" {
		t.Fatal("reused Option shares its receiver map")
	}
	c := NewOptions(opt)
	if c.labels["service"] != "orders" {
		t.Fatal("receiver mutation changed captured snapshot")
	}
}

func TestWithLabelsReplacementAndClear(t *testing.T) {
	o := NewOptions(
		WithLabels(map[string]string{"a": "1"}),
		WithLabels(map[string]string{"b": "2"}),
	)
	if len(o.labels) != 1 || o.labels["b"] != "2" {
		t.Fatalf("WithLabels must replace, not merge: %v", o.labels)
	}
	o.Apply(WithLabels(nil))
	if o.labels != nil || o.Labels() != nil {
		t.Fatal("nil labels must clear while preserving nil")
	}
}

func TestLabelsGetterReturnsCopy(t *testing.T) {
	o := NewOptions(WithLabels(map[string]string{"a": "1"}))
	copy := o.Labels()
	copy["a"] = "changed"
	if o.labels["a"] != "1" {
		t.Fatal("getter exposed internal map")
	}
}

// 只在测试中演示具体选项类型，不强迫生产选项全部改为结构体。
type fixedRetriesOption struct {
	value int
}

func (f fixedRetriesOption) apply(o *Options) {
	o.retries = f.value
}

func TestConcreteAndFunctionOptionsMix(t *testing.T) {
	o := NewOptions(
		WithTimeout(time.Second),
		fixedRetriesOption{value: 8},
		WithRetries(0),
	)
	if o.timeout != time.Second || o.retries != 0 {
		t.Fatalf("mixed implementations do not obey order: %+v", o)
	}
}

func TestOptionFuncRunsOnce(t *testing.T) {
	calls := 0
	o := NewOptions(OptionFunc(func(o *Options) {
		calls++
		o.retries = 9
	}))
	if calls != 1 || o.retries != 9 {
		t.Fatalf("adapter calls=%d retries=%d", calls, o.retries)
	}
}

func TestReusableOptionForIndependentConcurrentConfigs(t *testing.T) {
	opt := WithLabels(map[string]string{"service": "orders"})
	const workers = 16
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o := NewOptions(opt)
			if o.labels["service"] != "orders" {
				errs <- fmt.Errorf("unexpected snapshot: %v", o.labels)
				return
			}
			// 修改的是本次构造的独立 map，不是同一个 Options。
			o.labels["service"] = "local"
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if NewOptions(opt).labels["service"] != "orders" {
		t.Fatal("concurrent receivers changed captured snapshot")
	}
}
