package fileserver

import (
	"errors"
	"fmt"
	"io"
)

// Sentinel errors. Callers classify them with errors.Is only; drivers use
// fmt.Errorf("%w: %w", ErrXxx, cause) to keep both the sentinel and the
// underlying cause.
// 哨兵错误。调用方只用 errors.Is 判断；驱动用 fmt.Errorf("%w: %w", ErrXxx, cause)
// 同时保留哨兵与底层原因。
var (
	// ErrInvalidKey indicates a Key violates the Key rules | 表示 Key 不符合 Key 规则。
	ErrInvalidKey = errors.New("fileserver: invalid key")
	// ErrInvalidArgument indicates an invalid argument, option or range | 表示参数、选项或范围非法。
	ErrInvalidArgument = errors.New("fileserver: invalid argument")
	// ErrUnsupported indicates the backend cannot satisfy an option or capability; returned before any data is written | 表示后端不能满足该选项或能力；在写入任何数据之前返回。
	ErrUnsupported = errors.New("fileserver: unsupported")
	// ErrNotFound indicates the object does not exist (or the Key does not address a regular object) | 表示对象不存在（或 Key 不指向普通对象）。
	ErrNotFound = errors.New("fileserver: not found")
	// ErrConflict indicates a precondition failed, or the object was replaced while the operation was in flight; retrying is pointless.
	// A truncated transfer is not this error; see io.ErrUnexpectedEOF and ErrIntegrity.
	// ErrConflict 表示前置条件不满足，或对象在这次操作进行中被替换；重试无用。
	// 传输截断不属于它，见 io.ErrUnexpectedEOF 与 ErrIntegrity。
	ErrConflict = errors.New("fileserver: conflict")
	// ErrPermission indicates the backend denied access | 表示后端拒绝访问。
	ErrPermission = errors.New("fileserver: permission denied")
	// ErrThrottled indicates the backend throttled or is busy and rejected the request before processing it; retrying later with the same input is safe | 表示后端限流或繁忙，请求在处理前被拒绝；稍后用同样的输入重试是安全的。
	ErrThrottled = errors.New("fileserver: throttled")
	// ErrSizeMismatch indicates the declared length does not match the actual input | 表示声明长度与实际输入不符。
	ErrSizeMismatch = errors.New("fileserver: size mismatch")
	// ErrTooLarge indicates the object or a part exceeds the size limit | 表示对象或分片超过上限。
	ErrTooLarge = errors.New("fileserver: object too large")
	// ErrIntegrity indicates a content integrity check failed: the transfer was corrupted, or the backend's receipt does not match what was sent.
	// Restarting the whole operation with the same input is safe.
	// ErrIntegrity 表示内容完整性校验失败：传输损坏，或后端回执与发送的内容不符。
	// 用同样的输入重新发起整个操作是安全的。
	ErrIntegrity = errors.New("fileserver: integrity check failed")
	// ErrIndeterminate indicates the commit outcome is unknown: the object may already be in effect.
	// When returning it, do not retry automatically and do not treat "same Key exists" as success; verify with Stat and business-checkable information.
	// ErrIndeterminate 表示提交结果不确定：对象可能已经生效。
	// 返回它时不得自动重试，也不得把"同 Key 存在"当作成功；应通过 Stat 与业务可核验的信息判断。
	ErrIndeterminate = errors.New("fileserver: outcome indeterminate")
)

// Error attaches the operation name and Key to an error. Callers extract it
// with errors.As; classification still uses errors.Is.
// Error 为错误附加操作名与 Key。调用方用 errors.As 取出；判断类别仍用 errors.Is。
type Error struct {
	Op string // "Upload", "Download", ... | "Upload"、"Download"……
	// Key is the object Key the operation targeted, "" when not applicable | 操作涉及的对象 Key，不适用时为 ""。
	Key string
	// Err is the wrapped sentinel or cause | 被包装的哨兵错误或原因。
	Err error
}

// Error returns the formatted message | Error 返回格式化的错误消息.
func (e *Error) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("fileserver: %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("fileserver: %s %q: %v", e.Op, e.Key, e.Err)
}

// Unwrap returns the underlying error so errors.Is reaches both the sentinel
// and the original cause (including context errors).
// Unwrap 返回底层错误，使 errors.Is 能穿透到哨兵与原始原因（包括 context 错误）。
func (e *Error) Unwrap() error { return e.Err }

// invalidArgument builds an error wrapping ErrInvalidArgument. msg must not
// contain credentials or full local paths.
// invalidArgument 构造包装 ErrInvalidArgument 的错误。msg 不得包含凭据或完整本地路径。
func invalidArgument(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, msg)
}

// Retryable reports whether restarting the whole operation with the same input
// is safe: a truncated transfer (io.ErrUnexpectedEOF), ErrIntegrity and
// ErrThrottled return true; ErrIndeterminate and ErrConflict return false
// first even when the chain also contains a transfer error; unrecognized
// errors also return false, leaving the decision to the caller.
// Retryable 报告以同样的输入重新发起整个操作是否安全：
// 传输被截断（io.ErrUnexpectedEOF）、ErrIntegrity 与 ErrThrottled 返回 true；
// ErrIndeterminate 与 ErrConflict 优先返回 false，即使错误链中还包含传输错误；
// 未识别的错误也返回 false，由调用方自行判断。
func Retryable(err error) bool {
	if errors.Is(err, ErrIndeterminate) || errors.Is(err, ErrConflict) {
		return false
	}
	return errors.Is(err, ErrIntegrity) || errors.Is(err, ErrThrottled) || errors.Is(err, io.ErrUnexpectedEOF)
}
