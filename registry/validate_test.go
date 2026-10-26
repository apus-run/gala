package registry

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	var nilIns *ServiceInstance
	tests := []struct {
		name string
		ins  *ServiceInstance
		want error
	}{
		{"nil", nilIns, ErrNilInstance},
		{"empty name", &ServiceInstance{}, ErrInvalidName},
		{"slash in name", &ServiceInstance{Name: "a/b"}, ErrInvalidName},
		{"slash in id", &ServiceInstance{Name: "a", ID: "x/y"}, ErrInvalidID},
		{"empty id is allowed", &ServiceInstance{Name: "a"}, nil},
		{"ok", &ServiceInstance{Name: "edu.course.student-api", ID: "i-1"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.ins.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if len(a) != 32 || a == b {
		t.Fatalf("unexpected ids %q %q", a, b)
	}
	if err := ValidateID(a); err != nil {
		t.Fatal(err)
	}
}
