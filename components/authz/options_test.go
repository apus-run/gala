package authz

import (
	"testing"
	"time"

	fileadapter "github.com/casbin/casbin/v3/persist/file-adapter"
)

func TestDefaultOptionsAreIndependent(t *testing.T) {
	first, second := DefaultOptions(), NewOptions()
	if first.modelFilePath != "./model.conf" || second.modelFilePath != "./model.conf" || first.policyLoadTimeout != 5*time.Second || second.policyLoadTimeout != 5*time.Second {
		t.Fatal("unexpected defaults")
	}
	firstAdapter, firstOK := first.policyAdapter.(*fileadapter.Adapter)
	secondAdapter, secondOK := second.policyAdapter.(*fileadapter.Adapter)
	if !firstOK || !secondOK || firstAdapter == secondAdapter {
		t.Fatal("default policy adapters must be independent")
	}
	first.Apply(WithModelFilePath("custom.conf"), WithPolicyLoadTimeout(time.Hour))
	if second.modelFilePath != "./model.conf" || second.policyLoadTimeout != 5*time.Second {
		t.Fatal("default configuration was mutated")
	}
}

func TestOptionsApplyOverridesInOrder(t *testing.T) {
	options := NewOptions(
		WithModelFilePath("original.conf"),
		WithPolicyLoadTimeout(2*time.Hour),
		WithPolicyLoadTimeout(3*time.Hour),
	)
	if options.modelFilePath != "original.conf" || options.policyLoadTimeout != 3*time.Hour {
		t.Fatal("options were not applied in order")
	}
	options.Apply(WithModelFilePath("custom.conf"))
	options.Apply()
	if options.modelFilePath != "custom.conf" || options.policyLoadTimeout != 3*time.Hour {
		t.Fatal("Apply reset unrelated options")
	}
}

func TestOptionsValidateFinalStateWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  []Option
		valid bool
	}{
		{"defaults", nil, true},
		{"empty model", []Option{WithModelFilePath("")}, false},
		{"zero timeout", []Option{WithPolicyLoadTimeout(0)}, false},
		{"negative timeout", []Option{WithPolicyLoadTimeout(-time.Second)}, false},
		{"overridden timeout", []Option{WithPolicyLoadTimeout(0), WithPolicyLoadTimeout(time.Second)}, true},
		{"nil adapter", []Option{WithPolicyAdapter(nil)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := NewOptions(tc.opts...)
			before := options.modelFilePath
			timeout := options.policyLoadTimeout
			if err := options.Validate(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if options.modelFilePath != before || options.policyLoadTimeout != timeout {
				t.Fatal("Validate modified configuration")
			}
		})
	}
}
