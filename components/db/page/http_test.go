package page_test

import (
	"errors"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"testing"

	"github.com/apus-run/gala/components/db/page"
)

func TestParsePagination(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    page.Pagination
		wantErr bool
	}{
		{name: "defaults", want: page.Pagination{Limit: 20}},
		{name: "empty values", query: "offset=&limit=", want: page.Pagination{Limit: 20}},
		{name: "bare key", query: "limit", want: page.Pagination{Limit: 20}},
		{name: "offset only", query: "offset=15", want: page.Pagination{Offset: 15, Limit: 20}},
		{name: "limit only", query: "limit=10", want: page.Pagination{Limit: 10}},
		{name: "arbitrary offset", query: "offset=15&limit=10", want: page.Pagination{Offset: 15, Limit: 10}},
		{name: "minimum values", query: "offset=0&limit=1", want: page.Pagination{Limit: 1}},
		{name: "maximum limit", query: "limit=100", want: page.Pagination{Limit: 100}},
		{
			name: "maximum offset", query: "offset=" + strconv.Itoa(math.MaxInt),
			want: page.Pagination{Offset: math.MaxInt, Limit: 20},
		},
		{
			name: "other parameters are ignored", query: "page=invalid&size=-1&sort=-id&offset=15&limit=10",
			want: page.Pagination{Offset: 15, Limit: 10},
		},
		{
			name: "first values win", query: "offset=15&offset=-1&limit=10&limit=invalid",
			want: page.Pagination{Offset: 15, Limit: 10},
		},
		{name: "first empty value uses default", query: "limit=&limit=10", want: page.Pagination{Limit: 20}},
		{name: "negative offset", query: "offset=-1", wantErr: true},
		{name: "negative limit", query: "limit=-1", wantErr: true},
		{name: "zero limit", query: "limit=0", wantErr: true},
		{name: "oversized limit", query: "limit=101", wantErr: true},
		{name: "invalid offset", query: "offset=abc", wantErr: true},
		{name: "fractional limit", query: "limit=1.5", wantErr: true},
		{name: "whitespace", query: "offset=%201", wantErr: true},
		{name: "invalid first value", query: "limit=invalid&limit=10", wantErr: true},
		{name: "offset integer overflow", query: "offset=" + strconv.Itoa(math.MaxInt) + "0", wantErr: true},
		{name: "limit integer overflow", query: "limit=" + strconv.Itoa(math.MaxInt) + "0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			got, err := page.ParsePagination(values)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, page.ErrInvalidPagination) {
				t.Fatalf("error = %v, want ErrInvalidPagination", err)
			}
			if got != tt.want {
				t.Fatalf("ParsePagination() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParsePageParams(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    page.Params
		wantErr bool
	}{
		{name: "defaults", want: page.Params{Page: 1, Size: 20}},
		{name: "empty values", query: "page=&size=", want: page.Params{Page: 1, Size: 20}},
		{name: "bare key", query: "size", want: page.Params{Page: 1, Size: 20}},
		{name: "page only", query: "page=3", want: page.Params{Page: 3, Size: 20}},
		{name: "size only", query: "size=10", want: page.Params{Page: 1, Size: 10}},
		{name: "page and size", query: "page=3&size=10", want: page.Params{Page: 3, Size: 10}},
		{name: "minimum values", query: "page=1&size=1", want: page.Params{Page: 1, Size: 1}},
		{name: "maximum size", query: "size=100", want: page.Params{Page: 1, Size: 100}},
		{
			name: "maximum page is parsed without conversion", query: "page=" + strconv.Itoa(math.MaxInt),
			want: page.Params{Page: math.MaxInt, Size: 20},
		},
		{
			name: "other parameters are ignored", query: "offset=invalid&limit=-1&sort=-id&page=3&size=10",
			want: page.Params{Page: 3, Size: 10},
		},
		{
			name: "first values win", query: "page=3&page=-1&size=10&size=invalid",
			want: page.Params{Page: 3, Size: 10},
		},
		{name: "first empty value uses default", query: "size=&size=10", want: page.Params{Page: 1, Size: 20}},
		{name: "negative page", query: "page=-1", wantErr: true},
		{name: "zero page", query: "page=0", wantErr: true},
		{name: "negative size", query: "size=-1", wantErr: true},
		{name: "zero size", query: "size=0", wantErr: true},
		{name: "oversized size", query: "size=101", wantErr: true},
		{name: "invalid page", query: "page=abc", wantErr: true},
		{name: "fractional size", query: "size=1.5", wantErr: true},
		{name: "whitespace", query: "page=%201", wantErr: true},
		{name: "invalid first value", query: "size=invalid&size=10", wantErr: true},
		{name: "page integer overflow", query: "page=" + strconv.Itoa(math.MaxInt) + "0", wantErr: true},
		{name: "size integer overflow", query: "size=" + strconv.Itoa(math.MaxInt) + "0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			got, err := page.ParsePageParams(values)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, page.ErrInvalidPagination) {
				t.Fatalf("error = %v, want ErrInvalidPagination", err)
			}
			if got != tt.want {
				t.Fatalf("ParsePageParams() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPaginationParsersValues(t *testing.T) {
	tests := []struct {
		name   string
		values url.Values
	}{
		{name: "nil values"},
		{name: "keys without values", values: url.Values{"offset": nil, "limit": {}, "page": nil, "size": {}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := page.ParsePagination(tt.values)
			if err != nil || p != (page.Pagination{Limit: 20}) {
				t.Fatalf("ParsePagination() = %+v, %v", p, err)
			}
			params, err := page.ParsePageParams(tt.values)
			if err != nil || params != (page.Params{Page: 1, Size: 20}) {
				t.Fatalf("ParsePageParams() = %+v, %v", params, err)
			}
		})
	}
	t.Run("input is not mutated", func(t *testing.T) {
		values := url.Values{"offset": {"15"}, "limit": {"10"}, "page": {"3"}, "size": {"10"}}
		want := url.Values{"offset": {"15"}, "limit": {"10"}, "page": {"3"}, "size": {"10"}}
		if _, err := page.ParsePagination(values); err != nil {
			t.Fatal(err)
		}
		if _, err := page.ParsePageParams(values); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(values, want) {
			t.Fatalf("input changed: %v", values)
		}
	})
}

func TestParsePageParamsConversionOverflow(t *testing.T) {
	params, err := page.ParsePageParams(url.Values{"page": {strconv.Itoa(math.MaxInt)}})
	if err != nil {
		t.Fatalf("valid integer page must parse: %v", err)
	}
	p, err := page.FromPage(params.Page, params.Size)
	if err == nil || p != (page.Pagination{}) {
		t.Fatalf("FromPage() = %+v, %v, want zero pagination and overflow error", p, err)
	}
}
