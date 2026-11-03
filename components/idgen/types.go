// Package idgen defines the contract for unique ID generators | 定义唯一 ID 生成器的契约.
//
// # Notes | 说明
//
// The root package is interface-only: it declares IDGenerator and its error
// semantics and ships no implementation. Implementations live in the redis_gen
// subpackage; generated mocks for tests are under mocks/.
// 根包只有接口：声明 IDGenerator 及其错误语义，不含实现。实现在 redis_gen
// 子包，测试用 mock 在 mocks/。
//
// Contract highlights: GenMultiIDs rejects negative counts, zero returns an
// empty slice, and an error returns no partial batch although IDs may already
// be consumed; a cancelled or uncertain response must not reclaim a previously
// allocated interval.
// 契约要点：GenMultiIDs 拒绝负数，零返回空切片；出错不返回部分批次，但已
// 消耗的 ID 不会收回；取消或结果不确定时不得回收已分配的区间。
//
//go:generate mockgen -source=$GOFILE -destination=mocks/$GOFILE -package=mocks
package idgen

import (
	"context"
)

// IDGenerator returns IDs within the configured uniqueness domain.
// GenMultiIDs rejects negative counts; zero returns an empty slice.
// Errors return no partial batch, but may consume IDs. Cancellation or an
// uncertain response must not reclaim a previously allocated interval.
// IDGenerator 在配置的唯一性域内返回 ID。GenMultiIDs 拒绝负数，零返回空切片；
// 出错不返回部分批次，但可能消耗 ID；取消或结果不确定时不得回收已分配的区间。
type IDGenerator interface {
	// GenID returns the next unique ID | GenID 返回下一个唯一 ID。
	GenID(ctx context.Context) (int64, error)
	// GenMultiIDs returns a batch of counts unique IDs | GenMultiIDs 批量返回 counts 个唯一 ID。
	GenMultiIDs(ctx context.Context, counts int) ([]int64, error)
}
