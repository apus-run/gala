package conf

import (
	"context"
	"errors"
	"fmt"
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
	StageCanceled Stage = "canceled"
)

// Error is the only error type this package returns from Open and Reload.
// Its message is built from fixed fields, never from raw input.
//
// Error 是 Open 与 Reload 返回的唯一错误类型。消息只由固定字段拼成，
// 不包含原始输入。
type Error struct {
	Stage  Stage
	Source int    // source index, or -1 when unknown | 来源序号，未知为 -1
	Name   string // safe name: basename or env var name | 安全名：basename 或变量名
	Path   string // field path, optional | 字段路径，可选
	reason string // fixed diagnostic, never raw input
	detail string // business validation text only | 仅业务校验文本
	cause  error  // exposed only through Unwrap | 仅经 Unwrap 暴露
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
	if e.detail != "" {
		msg += ": " + e.detail
	}
	return msg
}

// Unwrap exposes only the sentinel causes that carry no input content.
// Unwrap 只暴露不含输入内容的哨兵原因。
func (e *Error) Unwrap() error {
	return e.cause
}

// newValidateError wraps a business Validate error. Per spec §6 the library
// does not redact it: Error() includes its text and Unwrap returns it, so the
// business must keep its messages free of configuration values.
// newValidateError 包装业务 Validate 的错误。按规格第 6 节，库不做脱敏：
// Error() 包含其文本，Unwrap 返回它，因此业务须保证消息不含配置值。
func newValidateError(err error) *Error {
	return &Error{Stage: StageValidate, Source: -1, cause: err, detail: err.Error()}
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

// wrapSourceErr converts an error from Source i into an *Error. It keeps the
// source index and a safe name when the source exposes one.
// wrapSourceErr 把来源 i 的错误转为 *Error，保留序号与安全名（若来源提供）。
func wrapSourceErr(i int, err error) *Error {
	if ce, ok := errors.AsType[*Error](err); ok {
		stage := ce.Stage
		switch stage {
		case StageRead, StageDecode, StageMerge, StageBind, StageConfig, StageCanceled:
		default:
			stage = StageRead
		}
		return newError(stage, i, ce.Name, ce.Path, err)
	}
	stage := StageRead
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		stage = StageCanceled
	}
	if de, ok := errors.AsType[*DecodeError](err); ok {
		stage = StageDecode
		err = de.Err
	}
	if me, ok := errors.AsType[*MappingError](err); ok {
		stage = StageConfig
		err = me.Err
	}
	name := ""
	var named interface{ SafeName() string }
	if errors.As(err, &named) {
		name = named.SafeName()
	}
	return newError(stage, i, name, "", err)
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

// SourceError constructs a redacted source error. Name must be a safe identifier
// (a basename or environment variable name), and path a field path, never a value.
// The core assigns the source index when loading the batch.
func SourceError(stage Stage, name, path string, cause error) *Error {
	return newError(stage, -1, name, path, cause)
}
