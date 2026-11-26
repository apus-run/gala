package dlock_test

import (
	"strings"
	"testing"
	"time"

	"github.com/apus-run/gala/components/dlock"
)

func TestNewOptions(t *testing.T) {
	o := dlock.NewOptions()

	if o.LockName != dlock.DefaultLockName {
		t.Fatalf("LockName = %q, want %q", o.LockName, dlock.DefaultLockName)
	}
	if o.LockTimeout != 10*time.Second {
		t.Fatalf("LockTimeout = %s, want 10s", o.LockTimeout)
	}
	if o.OperationTimeout != dlock.DefaultOperationTimeout {
		t.Fatalf("OperationTimeout = %s, want %s", o.OperationTimeout, dlock.DefaultOperationTimeout)
	}
	if dlock.DefaultOperationTimeout >= o.LockTimeout {
		t.Fatal("DefaultOperationTimeout must be shorter than the default LockTimeout")
	}
}

func TestApplyOptions(t *testing.T) {
	o := dlock.ApplyOptions(
		dlock.WithLockName("export"),
		dlock.WithLockTimeout(30*time.Second),
		dlock.WithOperationTimeout(2*time.Second),
		dlock.WithOwnerID("worker-1"),
	)

	if o.LockName != "export" {
		t.Fatalf("LockName = %q, want %q", o.LockName, "export")
	}
	if o.LockTimeout != 30*time.Second {
		t.Fatalf("LockTimeout = %s, want 30s", o.LockTimeout)
	}
	if o.OperationTimeout != 2*time.Second {
		t.Fatalf("OperationTimeout = %s, want 2s", o.OperationTimeout)
	}
	if o.OwnerID != "worker-1" {
		t.Fatalf("OwnerID = %q, want %q", o.OwnerID, "worker-1")
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    *dlock.Options
		wantErr string
	}{
		{
			name: "valid",
			opts: &dlock.Options{
				LockName:         "export",
				LockTimeout:      time.Second,
				OperationTimeout: dlock.DefaultOperationTimeout,
			},
		},
		{
			name:    "empty name",
			opts:    &dlock.Options{LockName: "", LockTimeout: time.Second},
			wantErr: "lock name",
		},
		{
			name:    "blank name",
			opts:    &dlock.Options{LockName: "  ", LockTimeout: time.Second},
			wantErr: "lock name",
		},
		{
			name: "sub-millisecond timeout",
			opts: &dlock.Options{
				LockName:         "export",
				LockTimeout:      500 * time.Microsecond,
				OperationTimeout: dlock.DefaultOperationTimeout,
			},
			wantErr: "shorter than 1ms",
		},
		{
			name: "zero timeout",
			opts: &dlock.Options{
				LockName:         "export",
				LockTimeout:      0,
				OperationTimeout: dlock.DefaultOperationTimeout,
			},
			wantErr: "shorter than 1ms",
		},
		{
			name: "valid operation timeout",
			opts: &dlock.Options{
				LockName:         "export",
				LockTimeout:      time.Second,
				OperationTimeout: dlock.DefaultOperationTimeout,
			},
		},
		{
			name:    "zero operation timeout",
			opts:    &dlock.Options{LockName: "export", LockTimeout: time.Second},
			wantErr: "operation timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
