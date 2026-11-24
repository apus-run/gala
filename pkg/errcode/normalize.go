package errcode

import (
	"context"
	"errors"
	"os"
)

// Mapper 根据完整的输入错误选择业务分类，无法识别时返回 nil。
// [Normalize] 采用返回值的公开字段，并将原始输入作为底层原因。Mapper 可以直接返回预定义错误。
// 多个请求共享同一个 Mapper 时，调用方负责其并发安全。
type Mapper func(err error) *AppError

// Normalize 为 HTTP 出口确定错误分类。nil 输入返回 nil，其他输入均返回非 nil 的 *AppError。
//
// 分类按以下优先级进行：
//  1. 用 [errors.AsType] 查找第一个 *AppError。如果匹配的是 nil 指针，使用 [ErrInternal]。
//  2. 按传入顺序调用非 nil 的 Mapper，采用第一个非 nil 结果。
//  3. [context.DeadlineExceeded]、[os.ErrDeadlineExceeded]，或首个 Timeout() bool 实现返回 true，使用 [ErrTimeout]。
//  4. [context.Canceled] 使用 [ErrCanceled]，其余错误使用 [ErrInternal]。
//
// 直接传入有效的 *AppError 会得到同一指针。对于包装或 [errors.Join] 的输入，
// 结果保留完整输入作为底层原因，供日志、[errors.Is] 和 [errors.As] 访问外层上下文及其他分支。
//
// 返回类型为 error 的业务函数应在成功时直接返回 nil。将 Normalize(nil) 转为 error 会得到含 nil 指针的非 nil 接口。
func Normalize(err error, mappers ...Mapper) *AppError {
	if err == nil {
		return nil
	}

	if e, ok := errors.AsType[*AppError](err); ok {
		if e == nil {
			return ErrInternal.Wrap(err)
		}
		if err == error(e) { //nolint:errorlint // 要判断输入本身就是 e，而不是链中包含 e
			return e
		}
		return e.Wrap(err)
	}

	for _, mapper := range mappers {
		if mapper == nil {
			continue
		}
		if mapped := mapper(err); mapped != nil {
			return mapped.Wrap(err)
		}
	}

	// 先识别标准库超时原因，避免外层 Timeout() == false 将其遮蔽。
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return ErrTimeout.Wrap(err)
	}
	// AsType 的类型参数必须实现 error，所以接口里要内嵌 error。
	if t, ok := errors.AsType[interface {
		error
		Timeout() bool
	}](err); ok && t.Timeout() {
		return ErrTimeout.Wrap(err)
	}
	if errors.Is(err, context.Canceled) {
		return ErrCanceled.Wrap(err)
	}
	return ErrInternal.Wrap(err)
}
