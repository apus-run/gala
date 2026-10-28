package where

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm/clause"
)

// ErrInvalidSort 表示排序参数不合法：空字段、单独的 "-"，或字段不在白名单内。
var ErrInvalidSort = errors.New("where: invalid sort")

// Sortable 把请求里的排序字段名映射到数据库列名。
//
// 它是排序的唯一安全边界：ORDER BY 的列名无法用占位符参数化，所以只有这里配置
// 的值会进入 SQL，请求带来的字符串永远不会。键区分大小写。
type Sortable map[string]string

// Parse 解析 "-created,name" 形式的排序参数。
//
// 逗号分段，段首尾空白去掉，"-" 前缀表示倒序。空参数返回 nil；空段、单独的
// "-"、白名单外的字段一律返回 ErrInvalidSort。同一个列重复出现只保留第一次，
// 因此输出长度不会超过白名单大小。
func (s Sortable) Parse(spec string) ([]clause.OrderByColumn, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}

	segments := strings.Split(spec, ",")
	columns := make([]clause.OrderByColumn, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))

	for _, segment := range segments {
		name := strings.TrimSpace(segment)

		desc := strings.HasPrefix(name, "-")
		if desc {
			// 前缀后的名字不再去空白：`- id` 不是合法字段，走白名单校验失败。
			name = name[1:]
		}
		if name == "" {
			return nil, fmt.Errorf("%w: %q", ErrInvalidSort, segment)
		}

		column, ok := s[name]
		if !ok || column == "" {
			// 列名为空的白名单项是配置错误，按非法处理，避免生成空列名。
			return nil, fmt.Errorf("%w: %q", ErrInvalidSort, segment)
		}
		if _, duplicated := seen[column]; duplicated {
			continue
		}
		seen[column] = struct{}{}

		columns = append(columns, clause.OrderByColumn{
			Column: clause.Column{Name: column},
			Desc:   desc,
		})
	}

	return columns, nil
}
