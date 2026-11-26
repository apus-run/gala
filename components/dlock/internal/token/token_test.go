package token

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	first, err := New("host-a")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	second, err := New("host-a")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if first == second {
		t.Fatal("New() returned the same token twice")
	}
	if !strings.HasPrefix(first, "host-a@") {
		t.Fatalf("New() = %q, want prefix %q", first, "host-a@")
	}
	if got := Owner(first); got != "host-a" {
		t.Fatalf("Owner(%q) = %q, want %q", first, got, "host-a")
	}
}

func TestOwner(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "token", value: "host@0123456789abcdef", want: "host"},
		{name: "owner with separator", value: "team@host@0123456789abcdef", want: "team@host"},
		{name: "legacy value", value: "hostname", want: "hostname"},
		{name: "empty", value: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Owner(tt.value); got != tt.want {
				t.Fatalf("Owner(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
