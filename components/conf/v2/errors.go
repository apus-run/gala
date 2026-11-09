package conf

import (
	"context"
	"errors"
	"fmt"
	"github.com/apus-run/gala/components/conf/v2/internal/diagnostic"
	"io/fs"
)

// Stage names the step of a reload that failed.
// Stage 表示重载失败的步骤。
type Stage string

const (
	StageRead     Stage = "read"
	StageDecode   Stage = "decode"
	StageMerge    Stage = "merge"
	StageBind     Stage = "bind"
	StageValidate Stage = "validate"
	StageConfig   Stage = "config"
	StageResolve  Stage = "resolve"
	StageWatch    Stage = "watch"
	StageCanceled Stage = "canceled"
)

// Error is the only error type this package returns from Open and Reload.
// Its message is built from fixed fields, never from raw input.
//
// Error 是 Open 与 Reload 返回的唯一错误类型。消息只由固定字段拼成，
// 不包含原始输入。
type Error struct {
	Stage  Stage
	Source int               // source index, or -1 when unknown | 来源序号，未知为 -1
	Name   string            // optional public source identifier
	Path   string            // field path, optional | 字段路径，可选
	decode diagnostic.Decode // fixed document failure supplied by built-in sources
	reason string            // fixed diagnostic, never raw input
	cause  error             // exposed only through Unwrap | 仅经 Unwrap 暴露
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("conf: %s failed", e.Stage)
	if e.Source >= 0 {
		msg += fmt.Sprintf(" (source %d)", e.Source)
	}
	if e.Name != "" {
		msg += fmt.Sprintf(" [%s]", e.Name)
	}
	if e.Path != "" {
		msg += fmt.Sprintf(" at %s", e.Path)
	}
	if e.reason != "" {
		msg += ": " + e.reason
	}
	return msg
}

// Unwrap exposes cancellation/file sentinels or the original business validation
// cause. The latter is for programmatic inspection, not safe display.
func (e *Error) Unwrap() error {
	return e.cause
}

// newValidateError preserves the business cause only for programmatic inspection.
func newValidateError(err error) *Error {
	return &Error{Stage: StageValidate, Source: -1, cause: err, reason: "validation rejected configuration"}
}

// Only internally generated merge errors are used as fixed diagnostics.
func newMergeError(source int, cause error) *Error {
	e := newError(StageMerge, source, "", "", nil)
	switch cause {
	case errNull, errSourceType, errCycle, errDepth:
		e.reason = cause.Error()
	}
	return e
}

func newConfigError(source int, reason string) *Error {
	return &Error{Stage: StageConfig, Source: source, reason: reason}
}

func newError(stage Stage, source int, name, path string, cause error) *Error {
	return &Error{Stage: stage, Source: source, Name: name, Path: path, cause: safeCause(cause)}
}

// safeCause keeps only causes that are safe to expose. Other errors are
// dropped; their text may contain input.
// safeCause 只保留可以暴露的原因。其他错误会被丢弃，因为其文本可能含输入。
func safeCause(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return fs.ErrNotExist
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	}
	return nil
}

// wrapSourceErr assigns the source index and discards untrusted metadata.
func wrapSourceErr(i int, err error) *Error {
	if errors.Is(err, context.Canceled) {
		return newError(StageCanceled, i, "", "", context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newError(StageCanceled, i, "", "", context.DeadlineExceeded)
	}
	stage := StageRead
	var decode diagnostic.Decode
	if ce, ok := errors.AsType[*Error](err); ok {
		switch ce.Stage {
		case StageRead, StageDecode, StageConfig:
			stage = ce.Stage
			if stage == StageDecode {
				decode = ce.decode
			}
		}
	} else if _, ok := errors.AsType[*DecodeError](err); ok {
		stage = StageDecode
	} else if _, ok := errors.AsType[*MappingError](err); ok {
		stage = StageConfig
	}
	// Source-provided names and paths have no trusted schema provenance.
	var cause error
	if stage == StageRead && errors.Is(err, fs.ErrNotExist) {
		cause = fs.ErrNotExist
	}
	result := newError(stage, i, "", "", cause)
	if decode != 0 {
		result.decode = decode
		result.reason = decode.Error()
	}
	return result
}

// DecodeError marks a failure to decode a source's bytes, so the core can
// report stage "decode" without inspecting the source's error text.
// DecodeError 标记来源字节解码失败，使核心无需解析来源的错误文本即可报告
// "decode" 阶段。
type DecodeError struct {
	Err error
}

func (e *DecodeError) Error() string { return "decode failed" }
func (e *DecodeError) Unwrap() error { return e.Err }

// MappingError marks an invalid source configuration (for example a duplicate
// env path), reported as stage "config": the mapping is wrong before any value
// is read, which is neither a read failure nor a bind failure. Its text must
// name only safe identifiers.
// MappingError 标记来源配置无效（例如 env 路径重复），报告为 "config" 阶段：
// 映射在读取任何值之前就已错误，既非读取失败也非绑定失败。其文本只能包含安全标识符。
type MappingError struct {
	Err error
}

func (e *MappingError) Error() string { return "invalid mapping" }
func (e *MappingError) Unwrap() error { return e.Err }

// SourceError marks a source failure. Names and paths are intentionally omitted:
// arbitrary source metadata cannot be established as safe to display.
// Only read, decode and config are accepted; cancellation causes take priority.
func SourceError(stage Stage, name, path string, cause error) *Error {
	if errors.Is(cause, context.Canceled) {
		return newError(StageCanceled, -1, "", "", context.Canceled)
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return newError(StageCanceled, -1, "", "", context.DeadlineExceeded)
	}
	switch stage {
	case StageRead, StageDecode, StageConfig:
	default:
		stage = StageRead
	}
	if stage == StageDecode {
		if code, ok := cause.(diagnostic.Decode); ok {
			return &Error{Stage: stage, Source: -1, decode: code, reason: code.Error()}
		}
	}
	if stage != StageRead {
		cause = nil
	}
	return newError(stage, -1, "", "", cause)
}
