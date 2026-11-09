// Package page defines pagination parameters, normalization and results.
// Package page 定义分页参数、归一规则与查询结果。
//
// where, repo and store share this single set of rules: defaults, the cap and
// offset overflow handling are defined only here. The package itself does not
// depend on gorm.
// where、repo、store 共用这一份规则，默认值、上限与偏移溢出只在这里定义。
// 包本身不依赖 gorm。
package page

import (
	"errors"
	"math"
)

const (
	// DefaultLimit is used when no positive limit is given | DefaultLimit 是未指定正条数时使用的默认值。
	DefaultLimit = 20
	// MaxLimit is the maximum number of rows per page | MaxLimit 是分页条数的上限。
	MaxLimit = 100

	// DefaultSize preserves the page-size name | DefaultSize 保留原页大小常量名称。
	DefaultSize = DefaultLimit
	// MaxSize preserves the page-size name | MaxSize 保留原页大小上限常量名称。
	MaxSize = MaxLimit
)

// Pagination describes a bounded offset-based query. Normalize it before use.
// Pagination 表示按偏移量读取有限条记录的查询参数，使用前调用 Normalize。
type Pagination struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// Normalize returns a copy with a nonnegative offset and a limit in [1, MaxLimit].
// A nonpositive limit uses DefaultLimit; zero never means an unbounded query.
// Normalize 返回归一后的副本：负偏移取 0，非正条数取默认值，超过上限的条数截断。
// 条数为零不表示无限制查询。
func (p Pagination) Normalize() Pagination {
	if p.Offset < 0 {
		p.Offset = 0
	}
	switch {
	case p.Limit <= 0:
		p.Limit = DefaultLimit
	case p.Limit > MaxLimit:
		p.Limit = MaxLimit
	}
	return p
}

// FromPage converts a one-based page number and size into normalized pagination.
// Page numbers below 1 use page 1; size follows Normalize. Offset overflow returns
// an error and a zero Pagination. HTTP callers should validate explicit input
// before calling this function when invalid values must be rejected.
// FromPage 将页码和条数转换成归一后的分页参数：页码小于 1 按第 1 页处理，条数
// 遵循 Normalize；偏移溢出时返回错误和零值。HTTP 层按需先校验显式输入。
func FromPage(page, size int) (Pagination, error) {
	p := (Pagination{Limit: size}).Normalize()
	if page <= 1 {
		return p, nil
	}
	if page-1 > math.MaxInt/p.Limit {
		return Pagination{}, errors.New("page: offset overflows int")
	}
	p.Offset = (page - 1) * p.Limit
	return p, nil
}

// Page contains one page of items and the total number of matching records.
// Total is a count, not a sentinel for an uncounted query.
// Page 包含一页数据和全部匹配记录的总数；Total 不用于表示未执行计数。
type Page[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Offset int   `json:"offset"`
	Limit  int   `json:"limit"`
}

// NewPage assembles a result using the pagination actually used by the query.
// It preserves the count and pagination, and converts nil items to an empty slice
// so JSON contains [] rather than null. Non-nil items are not copied or truncated.
// NewPage 使用查询实际使用的分页参数组装结果，保留总数和分页值，将 nil 数据
// 转为空切片以输出 JSON []；非 nil 数据不会被复制或截断。
func NewPage[T any](items []T, total int64, p Pagination) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{
		Items:  items,
		Total:  total,
		Offset: p.Offset,
		Limit:  p.Limit,
	}
}

// Params holds the page parameters.
//
// The zero value is valid: Page=0 and Size=0 are treated as page 1 with
// DefaultSize rows. Normalization happens inside Limit, Offset and NewMeta,
// so callers never need to normalize first; there is no "an unnormalized
// Params is unusable" precondition.
// Params 是页码参数。
//
// 零值合法：Page=0、Size=0 按第 1 页、DefaultSize 条处理。归一发生在 Limit、
// Offset、NewMeta 内部，调用方不需要先归一，也不存在「未归一的 Params 不可用」
// 这种前置条件。
type Params struct {
	Page int
	Size int
}

// Limit returns the normalized per-page size: Size<1 becomes DefaultSize and
// Size>MaxSize becomes MaxSize.
// Limit 返回归一后的每页条数：Size<1 取 DefaultSize，Size>MaxSize 取 MaxSize。
func (p Params) Limit() int {
	return (Pagination{Limit: p.Size}).Normalize().Limit
}

// Offset returns the normalized offset.
//
// When the page number is large enough to overflow the multiplication, it
// saturates at math.MaxInt, which means "past the last page" instead of a
// negative offset.
// Offset 返回归一后的偏移量。
//
// 页码大到乘法会溢出时饱和到 math.MaxInt，等价于「超出末页」，不会变成负偏移。
func (p Params) Offset() int {
	pagination, err := FromPage(p.Page, p.Size)
	if err != nil {
		return math.MaxInt
	}
	return pagination.Offset
}

// Meta is the pagination metadata; its JSON shape matches the former
// repo.PaginationMeta.
// Meta 是分页元数据，JSON 形状与原来的 repo.PaginationMeta 一致。
type Meta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// NewMeta builds metadata from the page parameters and the total count.
//
// TotalPages rounds up; HasNext is Page < TotalPages, so the last page never
// reports a next page by mistake.
// NewMeta 按页码参数与总数生成元数据。
//
// TotalPages 向上取整；HasNext 是 Page < TotalPages，末页不会误报有下一页。
func NewMeta(p Params, total int64) Meta {
	if total < 0 {
		total = 0
	}

	current := p.Page
	if current < 1 {
		current = 1
	}
	size := p.Limit()

	var totalPages int
	if total > 0 {
		totalPages = int(total / int64(size))
		if total%int64(size) > 0 {
			totalPages++
		}
	}

	return Meta{
		Page:       current,
		PerPage:    size,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    current < totalPages,
		HasPrev:    current > 1,
	}
}
