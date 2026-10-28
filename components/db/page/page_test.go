package page

import (
	"math"
	"testing"
)

func TestParamsLimit(t *testing.T) {
	tests := []struct {
		name string
		size int
		want int
	}{
		{name: "zero falls back to default", size: 0, want: DefaultSize},
		{name: "negative falls back to default", size: -5, want: DefaultSize},
		{name: "within range is kept", size: 42, want: 42},
		{name: "above max is clamped", size: MaxSize + 1, want: MaxSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Params{Page: 1, Size: tt.size}).Limit(); got != tt.want {
				t.Fatalf("Limit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParamsOffset(t *testing.T) {
	tests := []struct {
		name string
		page int
		size int
		want int
	}{
		{name: "first page", page: 1, size: 10, want: 0},
		{name: "zero params", page: 0, size: 0, want: 0},
		{name: "zero page is treated as first", page: 0, size: 10, want: 0},
		{name: "negative page is treated as first", page: -3, size: 10, want: 0},
		{name: "third page", page: 3, size: 20, want: 40},
		{name: "zero size uses the default", page: 2, size: 0, want: DefaultSize},
		{name: "oversized size is clamped", page: 2, size: MaxSize + 1, want: MaxSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Params{Page: tt.page, Size: tt.size}).Offset(); got != tt.want {
				t.Fatalf("Offset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParamsOffsetSaturates(t *testing.T) {
	got := (Params{Page: math.MaxInt, Size: MaxSize}).Offset()

	if got < 0 {
		t.Fatalf("Offset() = %d, must not be negative", got)
	}
	if got != math.MaxInt {
		t.Fatalf("Offset() = %d, want saturation at %d", got, math.MaxInt)
	}
}

func TestNewMeta(t *testing.T) {
	tests := []struct {
		name       string
		params     Params
		total      int64
		page       int
		perPage    int
		totalPages int
		hasNext    bool
		hasPrev    bool
	}{
		{
			name:       "empty result",
			params:     Params{Page: 1, Size: 10},
			total:      0,
			page:       1,
			perPage:    10,
			totalPages: 0,
			hasNext:    false,
			hasPrev:    false,
		},
		{
			name:       "first of many",
			params:     Params{Page: 1, Size: 10},
			total:      25,
			page:       1,
			perPage:    10,
			totalPages: 3,
			hasNext:    true,
			hasPrev:    false,
		},
		{
			name:       "last page has no next",
			params:     Params{Page: 3, Size: 10},
			total:      25,
			page:       3,
			perPage:    10,
			totalPages: 3,
			hasNext:    false,
			hasPrev:    true,
		},
		{
			name:       "exact multiple",
			params:     Params{Page: 10, Size: 10},
			total:      100,
			page:       10,
			perPage:    10,
			totalPages: 10,
			hasNext:    false,
			hasPrev:    true,
		},
		{
			name:       "page beyond the end",
			params:     Params{Page: 99, Size: 10},
			total:      25,
			page:       99,
			perPage:    10,
			totalPages: 3,
			hasNext:    false,
			hasPrev:    true,
		},
		{
			name:       "zero params are normalized",
			params:     Params{},
			total:      1,
			page:       1,
			perPage:    DefaultSize,
			totalPages: 1,
			hasNext:    false,
			hasPrev:    false,
		},
		{
			name:       "negative total is treated as zero",
			params:     Params{Page: 1, Size: 10},
			total:      -5,
			page:       1,
			perPage:    10,
			totalPages: 0,
			hasNext:    false,
			hasPrev:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewMeta(tt.params, tt.total)

			wantTotal := tt.total
			if wantTotal < 0 {
				wantTotal = 0
			}
			if got.Total != wantTotal {
				t.Fatalf("Total = %d, want %d", got.Total, wantTotal)
			}
			if got.Page != tt.page || got.PerPage != tt.perPage || got.TotalPages != tt.totalPages {
				t.Fatalf("NewMeta = %#v, want page=%d perPage=%d totalPages=%d",
					got, tt.page, tt.perPage, tt.totalPages)
			}
			if got.HasNext != tt.hasNext || got.HasPrev != tt.hasPrev {
				t.Fatalf("NewMeta = %#v, want hasNext=%v hasPrev=%v", got, tt.hasNext, tt.hasPrev)
			}
		})
	}
}

// total 接近 int64 上限时，ceil 不能因为 total+size-1 溢出而变成负数。
func TestNewMetaLargeTotal(t *testing.T) {
	meta := NewMeta(Params{Page: 1, Size: 10}, math.MaxInt64)

	if meta.Total != math.MaxInt64 {
		t.Fatalf("Total = %d, want %d", meta.Total, int64(math.MaxInt64))
	}
	if meta.TotalPages <= 0 {
		t.Fatalf("TotalPages = %d, must stay positive", meta.TotalPages)
	}
}
