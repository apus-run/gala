package id

import (
	"context"
	"testing"
	"time"
)

func TestSonyflake(t *testing.T) {
	sf := NewSonyflake(WithSonyflakeMachineId(1))
	t.Log(sf.Id(context.Background()))
}

func TestNewSonyflake_DefaultOptions(t *testing.T) {
	sf := NewSonyflake()
	if sf == nil {
		t.Fatal("expected non-nil Sonyflake instance")
	}
	if sf.Error != nil {
		t.Fatalf("unexpected error: %v", sf.Error)
	}
	id, err := sf.NextID()
	if err != nil {
		t.Fatalf("NextID failed: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}
}

func TestNewSonyflake_WithCustomMachineID(t *testing.T) {
	sf := NewSonyflake(WithSonyflakeMachineId(2))
	if sf == nil {
		t.Fatal("expected non-nil Sonyflake instance")
	}
	if sf.Error != nil {
		t.Fatalf("unexpected error: %v", sf.Error)
	}
	id, err := sf.NextID()
	if err != nil {
		t.Fatalf("NextID failed: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}
}

func TestNewSonyflake_InvalidStartTime(t *testing.T) {
	// Set startTime to future to trigger "invalid start time"
	sf := NewSonyflake(func(opts *SonyflakeOptions) {
		opts.startTime = time.Now().Add(time.Hour)
	})
	if sf == nil {
		t.Fatal("expected non-nil Sonyflake instance")
	}
	if sf.Error == nil {
		t.Error("expected error due to invalid start time, got nil")
	}
}

func TestCheckedConstructionAndCancellation(t *testing.T) {
	for _, machine := range []int{-1, 65536} {
		if _, err := NewSonyflakeChecked(WithSonyflakeMachineId(machine)); err == nil {
			t.Fatalf("accepted machine %d", machine)
		}
	}
	s, err := NewSonyflakeChecked()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := s.NextIDContext(ctx); n != 0 || err != context.Canceled {
		t.Fatalf("canceled generation: %d %v", n, err)
	}
}

func TestSonyflakeMachineIDCompatibility(t *testing.T) {
	cases := []struct {
		name         string
		ids          []int
		legacy       uint64
		checked      uint64
		legacyError  bool
		checkedError bool
	}{
		{name: "default", legacy: 1, checked: 1},
		{name: "zero", ids: []int{0}, legacy: 1, checked: 0},
		{name: "negative", ids: []int{-1}, legacy: 1, checkedError: true},
		{name: "positive then zero", ids: []int{7, 0}, legacy: 7, checked: 0},
		{name: "positive then negative", ids: []int{7, -1}, legacy: 7, checkedError: true},
		{name: "zero then positive", ids: []int{0, 7}, legacy: 7, checked: 7},
		{name: "negative then positive", ids: []int{-1, 7}, legacy: 7, checked: 7},
		{name: "maximum", ids: []int{65535}, legacy: 65535, checked: 65535},
		{name: "too large", ids: []int{65536}, legacyError: true, checkedError: true},
		{name: "positive then too large", ids: []int{7, 65536}, legacyError: true, checkedError: true},
		{name: "too large then positive", ids: []int{65536, 7}, legacy: 7, checked: 7},
		{name: "too large then zero", ids: []int{65536, 0}, legacyError: true, checked: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := make([]func(*SonyflakeOptions), 0, len(tc.ids))
			for _, machine := range tc.ids {
				opts = append(opts, WithSonyflakeMachineId(machine))
			}
			assertMachine := func(s *Sonyflake, want uint64) {
				t.Helper()
				n, err := s.NextID()
				if err != nil {
					t.Fatal(err)
				}
				if got := n & 65535; got != want {
					t.Fatalf("machine bits: got %d want %d", got, want)
				}
			}
			checkStrict := func() {
				t.Helper()
				s, err := NewSonyflakeChecked(opts...)
				if tc.checkedError {
					if err == nil || s != nil {
						t.Fatalf("checked constructor accepted invalid final value: %v %v", s, err)
					}
					return
				}
				if err != nil || s == nil {
					t.Fatalf("checked constructor failed: %v", err)
				}
				assertMachine(s, tc.checked)
			}
			// Reuse the exact closures in both directions. Checked behavior must
			// not leak into legacy calls, nor be disabled by a legacy call.
			checkStrict()
			s := NewSonyflake(opts...)
			if s == nil {
				t.Fatal("legacy constructor must return a wrapper even on error")
			}
			if tc.legacyError {
				if s.Error == nil {
					t.Fatal("legacy constructor accepted an out-of-range positive ID")
				}
			} else {
				if s.Error != nil {
					t.Fatal(s.Error)
				}
				assertMachine(s, tc.legacy)
			}
			checkStrict()
		})
	}
}
