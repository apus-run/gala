package where

import (
	"errors"
	"testing"

	"gorm.io/gorm/clause"
)

func TestSortableParse(t *testing.T) {
	sortable := Sortable{
		"id":         "id",
		"name":       "user_name",
		"created_at": "created_at",
	}

	tests := []struct {
		name string
		spec string
		want []clause.OrderByColumn
	}{
		{
			name: "empty spec means no ordering",
			spec: "",
			want: nil,
		},
		{
			name: "ascending",
			spec: "name",
			want: []clause.OrderByColumn{{Column: clause.Column{Name: "user_name"}}},
		},
		{
			name: "descending",
			spec: "-created_at",
			want: []clause.OrderByColumn{{Column: clause.Column{Name: "created_at"}, Desc: true}},
		},
		{
			name: "multiple with surrounding spaces",
			spec: " name , -id ",
			want: []clause.OrderByColumn{
				{Column: clause.Column{Name: "user_name"}},
				{Column: clause.Column{Name: "id"}, Desc: true},
			},
		},
		{
			name: "duplicate column keeps the first direction",
			spec: "id,-id",
			want: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sortable.Parse(tt.spec)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.spec, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", tt.spec, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Parse(%q)[%d] = %#v, want %#v", tt.spec, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSortableParseRejectsBadSpec(t *testing.T) {
	sortable := Sortable{"id": "id", "name": "name"}

	// 每一个用例都必须返回错误，且不能 panic。
	specs := []string{
		"password",
		"id,,name",
		",",
		"-",
		"id,",
		"name;drop table users",
		"(select 1)",
		"id)--",
		"NAME", // 白名单区分大小写
		"- id", // 前缀后的空白不再去掉
	}

	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			got, err := sortable.Parse(spec)
			if !errors.Is(err, ErrInvalidSort) {
				t.Fatalf("Parse(%q) error = %v, want ErrInvalidSort", spec, err)
			}
			if got != nil {
				t.Fatalf("Parse(%q) = %#v, want nil on error", spec, got)
			}
		})
	}
}

func TestSortableParseWithNilMap(t *testing.T) {
	var sortable Sortable

	if cols, err := sortable.Parse(""); err != nil || cols != nil {
		t.Fatalf("empty spec on nil Sortable = %#v, %v", cols, err)
	}
	if _, err := sortable.Parse("id"); !errors.Is(err, ErrInvalidSort) {
		t.Fatalf("nil Sortable must reject every field, got %v", err)
	}
}

func TestSortableParseEmptyColumn(t *testing.T) {
	sortable := Sortable{"broken": ""}

	if _, err := sortable.Parse("broken"); !errors.Is(err, ErrInvalidSort) {
		t.Fatalf("empty column must be rejected, got %v", err)
	}
}

// 白名单里出现空键名时，空段不能被当成合法字段。
func TestSortableParseEmptyKeyIsNotWhitelisted(t *testing.T) {
	sortable := Sortable{"": "id"}

	for _, spec := range []string{",", "a,,b", "-"} {
		if _, err := sortable.Parse(spec); !errors.Is(err, ErrInvalidSort) {
			t.Fatalf("Parse(%q) error = %v, want ErrInvalidSort", spec, err)
		}
	}
}
