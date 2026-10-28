package mocks

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/apus-run/gala/components/cache"
)

// 生成的 mock 必须实现当前的 cache.Cache。接口新增方法后忘了重新生成，这里编译失败
// （Deletes 和 GetAny 加进接口时就漏过一次）。
func TestMockCacheImplementsInterface(t *testing.T) {
	var c cache.Cache = NewMockCache(gomock.NewController(t))
	_ = c
}
