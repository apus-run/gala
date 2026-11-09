package page

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func TestPaginationNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input Pagination
		want  Pagination
	}{
		{name: "zero value", input: Pagination{}, want: Pagination{Limit: 20}},
		{name: "negative values", input: Pagination{Offset: -1, Limit: -1}, want: Pagination{Limit: 20}},
		{name: "arbitrary offset", input: Pagination{Offset: 15, Limit: 10}, want: Pagination{Offset: 15, Limit: 10}},
		{name: "minimum limit", input: Pagination{Limit: 1}, want: Pagination{Limit: 1}},
		{name: "maximum limit", input: Pagination{Limit: 100}, want: Pagination{Limit: 100}},
		{name: "clamped limit", input: Pagination{Offset: 15, Limit: 101}, want: Pagination{Offset: 15, Limit: 100}},
		{
			name:  "integer extremes",
			input: Pagination{Offset: math.MinInt, Limit: math.MinInt},
			want:  Pagination{Limit: 20},
		},
		{
			name:  "large offset is preserved",
			input: Pagination{Offset: math.MaxInt, Limit: math.MaxInt},
			want:  Pagination{Offset: math.MaxInt, Limit: 100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.input
			got := input.Normalize()
			if got != tt.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, tt.want)
			}
			if input != tt.input {
				t.Fatal("Normalize changed its receiver")
			}
			if got.Normalize() != got {
				t.Fatal("Normalize is not idempotent")
			}
		})
	}
}

func TestFromPage(t *testing.T) {
	tests := []struct {
		name    string
		page    int
		size    int
		want    Pagination
		wantErr bool
	}{
		{name: "zero value", want: Pagination{Limit: 20}},
		{name: "first page", page: 1, size: 10, want: Pagination{Limit: 10}},
		{name: "third page", page: 3, size: 20, want: Pagination{Offset: 40, Limit: 20}},
		{name: "default size", page: 2, want: Pagination{Offset: 20, Limit: 20}},
		{name: "negative size", page: 2, size: -1, want: Pagination{Offset: 20, Limit: 20}},
		{name: "clamp before conversion", page: 3, size: 1000, want: Pagination{Offset: 200, Limit: 100}},
		{name: "negative page", page: math.MinInt, size: 10, want: Pagination{Limit: 10}},
		{
			name: "largest page with one row", page: math.MaxInt, size: 1,
			want: Pagination{Offset: math.MaxInt - 1, Limit: 1},
		},
		{
			name: "last representable offset", page: math.MaxInt/100 + 1, size: 100,
			want: Pagination{Offset: (math.MaxInt / 100) * 100, Limit: 100},
		},
		{name: "first overflowing page", page: math.MaxInt/100 + 2, size: 100, wantErr: true},
		{name: "overflow with default size", page: math.MaxInt, wantErr: true},
		{
			name: "size is capped before overflow check", page: math.MaxInt/100 + 1, size: math.MaxInt,
			want: Pagination{Offset: (math.MaxInt / 100) * 100, Limit: 100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromPage(tt.page, tt.size)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FromPage() error = %v, want error %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("FromPage() = %+v, want %+v", got, tt.want)
			}

			// The existing API shares normalization but keeps its saturation contract.
			// 旧 API 共用归一规则，但保留偏移溢出时饱和的兼容行为。
			legacy := Params{Page: tt.page, Size: tt.size}
			if tt.wantErr {
				if legacy.Offset() != math.MaxInt {
					t.Fatal("legacy offset must saturate on overflow")
				}
				return
			}
			if legacy.Offset() != got.Offset || legacy.Limit() != got.Limit {
				t.Fatal("legacy pagination disagrees with FromPage")
			}
		})
	}
}

func TestNewPage(t *testing.T) {
	tests := []struct {
		name       string
		items      []int
		total      int64
		pagination Pagination
	}{
		{name: "empty result", pagination: Pagination{Limit: 20}},
		{name: "beyond last page", total: 10, pagination: Pagination{Offset: 100, Limit: 20}},
		{
			name: "partial page", items: []int{16, 17}, total: 17,
			pagination: Pagination{Offset: 15, Limit: 10},
		},
		{
			name: "large total", items: []int{1}, total: math.MaxInt64,
			pagination: Pagination{Limit: 20},
		},
		{
			name: "preserve actual query limit", items: []int{1, 2}, total: 2,
			pagination: Pagination{Limit: 200},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPage(tt.items, tt.total, tt.pagination)
			if got.Items == nil || !slices.Equal(got.Items, tt.items) {
				t.Fatalf("Items = %#v, want non-nil equivalent of %#v", got.Items, tt.items)
			}
			if got.Total != tt.total || got.Offset != tt.pagination.Offset || got.Limit != tt.pagination.Limit {
				t.Fatalf("NewPage() changed query metadata: %+v", got)
			}
		})
	}
}

func TestNewPageJSON(t *testing.T) {
	result := NewPage[int](nil, 5, Pagination{Offset: 15, Limit: 10})
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"items":[],"total":5,"offset":15,"limit":10}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s, want %s", encoded, want)
	}
}

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
