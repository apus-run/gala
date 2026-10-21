package fileserver

import (
	"errors"
	"fmt"
	"io"
)

// 哨兵错误。调用方只用 errors.Is 判断；驱动用 fmt.Errorf("%w: %w", ErrXxx, cause)
// 同时保留哨兵与底层原因。
var (
	// ErrInvalidKey 表示 Key 不符合 Key 规则。
	ErrInvalidKey = errors.New("fileserver: invalid key")
	// ErrInvalidArgument 表示参数、选项或范围非法。
	ErrInvalidArgument = errors.New("fileserver: invalid argument")
	// ErrUnsupported 表示后端不能满足该选项或能力；在写入任何数据之前返回。
	ErrUnsupported = errors.New("fileserver: unsupported")
	// ErrNotFound 表示对象不存在（或 Key 不指向普通对象）。
	ErrNotFound = errors.New("fileserver: not found")
	// ErrConflict 表示前置条件不满足，或对象在这次操作进行中被替换；重试无用。
	// 传输截断不属于它，见 io.ErrUnexpectedEOF 与 ErrIntegrity。
	ErrConflict = errors.New("fileserver: conflict")
	// ErrPermission 表示后端拒绝访问。
	ErrPermission = errors.New("fileserver: permission denied")
	// ErrSizeMismatch 表示声明长度与实际输入不符。
	ErrSizeMismatch = errors.New("fileserver: size mismatch")
	// ErrTooLarge 表示对象或分片超过上限。
	ErrTooLarge = errors.New("fileserver: object too large")
	// ErrIntegrity 表示内容完整性校验失败：传输损坏，或后端回执与发送的内容不符。
	// 用同样的输入重新发起整个操作是安全的。
	ErrIntegrity = errors.New("fileserver: integrity check failed")
	// ErrIndeterminate 表示提交结果不确定：对象可能已经生效。
	// 返回它时不得自动重试，也不得把"同 Key 存在"当作成功；应通过 Stat 与业务可核验的信息判断。
	ErrIndeterminate = errors.New("fileserver: outcome indeterminate")
)

// Error 为错误附加操作名与 Key。调用方用 errors.As 取出；判断类别仍用 errors.Is。
type Error struct {
	Op  string // "Upload"、"Download"……
	Key string
	Err error
}

func (e *Error) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("fileserver: %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("fileserver: %s %q: %v", e.Op, e.Key, e.Err)
}

// Unwrap 返回底层错误，使 errors.Is 能穿透到哨兵与原始原因（包括 context 错误）。
func (e *Error) Unwrap() error { return e.Err }

// invalidArgument 构造包装 ErrInvalidArgument 的错误。msg 不得包含凭据或完整本地路径。
func invalidArgument(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, msg)
}

// Retryable 报告以同样的输入重新发起整个操作是否安全：
// 传输被截断（io.ErrUnexpectedEOF）与 ErrIntegrity 返回 true；
// ErrIndeterminate 与 ErrConflict 优先返回 false，即使错误链中还包含传输错误；
// 未识别的错误也返回 false，由调用方自行判断。
func Retryable(err error) bool {
	if errors.Is(err, ErrIndeterminate) || errors.Is(err, ErrConflict) {
		return false
	}
	return errors.Is(err, ErrIntegrity) || errors.Is(err, io.ErrUnexpectedEOF)
}
