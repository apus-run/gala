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

// Fallback 返回一个把所有错误都归类为 e 的 Mapper，用作 Mapper 列表末尾的兜底。
// 错误链中已有的 *AppError 和前面的 Mapper 仍然优先，只有未识别的错误才落到 e。
// e 为 nil 时返回 nil，[Normalize] 会跳过这个 Mapper，未识别的错误仍归为 [ErrInternal]。
// 原错误会作为底层原因保留，日志和 errors.Is 仍可访问。
func Fallback(e *AppError) Mapper {
	if e == nil {
		return nil
	}
	return func(error) *AppError { return e }
}

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

// HTTPStatus 返回 err 最终会使用的 HTTP 状态码。
// nil 返回 200，其余错误按 [Normalize] 分类后再取 [AppError.HTTPStatus]。
// 需要在日志或指标里按 4xx/5xx 分级时可以直接使用，不必先构造完整响应体。
func HTTPStatus(err error) int {
	return Normalize(err).HTTPStatus()
}
