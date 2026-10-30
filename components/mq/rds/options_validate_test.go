package rds

import (
	"testing"
	"time"
)

func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name string
		o    Options
		ok   bool
	}{
		{"defaults", DefaultOptions(), true},
		{"zero retries allowed", Options{maxRetries: 0, pollInterval: time.Millisecond, claimIdle: time.Millisecond}, true},
		{"negative retries", Options{maxRetries: -1, pollInterval: time.Second, claimIdle: time.Minute}, false},
		{"poll below one millisecond", Options{maxRetries: 3, pollInterval: time.Microsecond, claimIdle: time.Minute}, false},
		{"claim idle below one millisecond", Options{maxRetries: 3, pollInterval: time.Second, claimIdle: time.Microsecond}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.o.Validate(); (err == nil) != tt.ok {
				t.Fatalf("Validate() error = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}
