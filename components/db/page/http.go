package page

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// ErrInvalidPagination identifies invalid integer syntax or out-of-range input.
// Callers can use errors.Is to map it to HTTP 400.
// ErrInvalidPagination 表示非法整数或越界输入，调用方可通过 errors.Is 将其映射为 HTTP 400。
var ErrInvalidPagination = errors.New("page: invalid pagination")

// ParsePagination parses offset/limit. Missing or empty values default to offset 0
// and DefaultLimit rows. Offset must be nonnegative; limit must be in [1, MaxLimit].
// Like url.Values.Get, it uses the first value of repeated parameters. Other keys
// are ignored; selecting a pagination mode is the caller's responsibility.
// ParsePagination 只解析 offset/limit：缺省或空值取偏移 0、条数 DefaultLimit。
// 偏移须非负，条数须在 [1, MaxLimit] 内；重复参数取首值，忽略其他键，由调用方选择分页模式。
func ParsePagination(values url.Values) (Pagination, error) {
	p := Pagination{Limit: DefaultLimit}
	var err error
	if raw := values.Get("offset"); raw != "" {
		p.Offset, err = strconv.Atoi(raw)
		if err != nil || p.Offset < 0 {
			return Pagination{}, fmt.Errorf("%w: invalid offset %q", ErrInvalidPagination, raw)
		}
	}
	if raw := values.Get("limit"); raw != "" {
		p.Limit, err = strconv.Atoi(raw)
		if err != nil || p.Limit < 1 || p.Limit > MaxLimit {
			return Pagination{}, fmt.Errorf("%w: invalid limit %q", ErrInvalidPagination, raw)
		}
	}
	return p, nil
}

// ParsePageParams parses one-based page/size. Missing or empty values default to
// page 1 and DefaultLimit rows. Page must be positive; size must be in [1, MaxLimit].
// Repeated parameters use the first value; other keys are ignored. Use FromPage
// to convert the result to Pagination and check offset overflow.
// ParsePageParams 只解析 page/size：缺省或空值取第 1 页、条数 DefaultLimit。
// 页码须为正数，条数须在 [1, MaxLimit] 内；重复参数取首值，忽略其他键。
// 使用 FromPage 转换为 Pagination 并检查偏移溢出。
func ParsePageParams(values url.Values) (Params, error) {
	p := Params{Page: 1, Size: DefaultLimit}
	var err error
	if raw := values.Get("page"); raw != "" {
		p.Page, err = strconv.Atoi(raw)
		if err != nil || p.Page < 1 {
			return Params{}, fmt.Errorf("%w: invalid page %q", ErrInvalidPagination, raw)
		}
	}
	if raw := values.Get("size"); raw != "" {
		p.Size, err = strconv.Atoi(raw)
		if err != nil || p.Size < 1 || p.Size > MaxLimit {
			return Params{}, fmt.Errorf("%w: invalid size %q", ErrInvalidPagination, raw)
		}
	}
	return p, nil
}
