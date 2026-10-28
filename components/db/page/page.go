// Package page 定义页码参数的归一规则与分页元数据。
//
// where、repo、store 共用这一份规则，默认值、上限与偏移溢出只在这里定义。
// 包本身不依赖 gorm。
package page

import "math"

const (
	// DefaultSize 是未指定每页条数时使用的值。
	DefaultSize = 20
	// MaxSize 是每页条数的上限。
	MaxSize = 100
)

// Params 是页码参数。
//
// 零值合法：Page=0、Size=0 按第 1 页、DefaultSize 条处理。归一发生在 Limit、
// Offset、NewMeta 内部，调用方不需要先归一，也不存在「未归一的 Params 不可用」
// 这种前置条件。
type Params struct {
	Page int
	Size int
}

// Limit 返回归一后的每页条数：Size<1 取 DefaultSize，Size>MaxSize 取 MaxSize。
func (p Params) Limit() int {
	switch {
	case p.Size < 1:
		return DefaultSize
	case p.Size > MaxSize:
		return MaxSize
	default:
		return p.Size
	}
}

// Offset 返回归一后的偏移量。
//
// 页码大到乘法会溢出时饱和到 math.MaxInt，等价于「超出末页」，不会变成负偏移。
func (p Params) Offset() int {
	size := p.Limit()
	if p.Page < 2 {
		return 0
	}
	if p.Page-1 > math.MaxInt/size {
		return math.MaxInt
	}
	return (p.Page - 1) * size
}

// Meta 是分页元数据，JSON 形状与原来的 repo.PaginationMeta 一致。
type Meta struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

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
