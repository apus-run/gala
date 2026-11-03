// Package validator validates structs with go-playground/validator/v10 and
// translates failures into Chinese messages | validator 基于 validator/v10
// 校验结构体，并把错误翻译为中文消息.
//
// # Notes | 说明
//
// NewValidator builds the engine once per process (sync.Once): later calls
// return the same *Validator and their options are ignored. Non-struct values
// pass as valid without error. On failure the field messages are translated
// with the built-in zh locale, stripped of the struct name, and joined with
// ";" into a single error. The package-level helpers (ValidateStruct, ...)
// use that singleton and must not be called before NewValidator or V, or they
// dereference nil.
// NewValidator 通过 sync.Once 每进程只构建一次：后续调用返回同一个 *Validator，
// 传入的选项被忽略。非结构体值直接视为合法。校验失败时字段消息用内置中文语言包
// 翻译、去掉结构体名，并以 ";" 拼接为单个错误。包级函数（ValidateStruct 等）使用
// 该单例，必须先调用过 NewValidator 或 V，否则会解引用空指针。
//
// # Example | 示例
//
//	v := validator.NewValidator()
//	if err := v.Validate(req); err != nil {
//		return err // e.g. "Name为必填字段;Age必须大于18"
//	}
package validator

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhcn "github.com/go-playground/validator/v10/translations/zh"
)

var (
	once sync.Once
	vd   *Validator
)

// Validator wraps the validation engine with a zh translator | Validator
// 用中文翻译器包裹校验引擎.
type Validator struct {
	validate   *validator.Validate
	translator ut.Translator
}

// Validate validates a struct and returns the zh-translated failures as one
// error | Validate 校验结构体，把失败项翻译为中文并合并为一个错误.
func (v *Validator) Validate(s any) error {
	return v.valid(s)
}

// ValidateContext validates a struct with a Context | ValidateContext 校验
// 结构体，带 Context.
func (v *Validator) ValidateContext(ctx context.Context, s any) error {
	return v.validCtx(ctx, s)
}

// ValidatePartial validates only the fields whose names are in partials
// | ValidatePartial 只校验 partials 中列出的字段.
func (v *Validator) ValidatePartial(s any, partials map[string]struct{}) error {
	return v.validate.StructPartial(s, slices.Collect(maps.Keys(partials))...)
}

func (v *Validator) valid(obj any) error {
	if reflect.Indirect(reflect.ValueOf(obj)).Kind() != reflect.Struct {
		return nil
	}

	e := v.validate.Struct(obj)
	if e != nil {
		err, ok := errors.AsType[validator.ValidationErrors](e)
		if !ok {
			return e
		}
		return removeStructName(err.Translate(v.translator))
	}
	return nil
}

func (v *Validator) validCtx(ctx context.Context, obj any) error {
	if reflect.Indirect(reflect.ValueOf(obj)).Kind() != reflect.Struct {
		return nil
	}

	e := v.validate.StructCtx(ctx, obj)
	if e != nil {
		err, ok := errors.AsType[validator.ValidationErrors](e)
		if !ok {
			return e
		}
		return removeStructName(err.Translate(v.translator))
	}
	return nil
}

func removeStructName(fields map[string]string) error {
	errs := slices.Collect(maps.Values(fields))
	return errors.New(strings.Join(errs, ";"))
}

// NewValidator builds the process-wide singleton validator, applying opts
// only on the first call | NewValidator 构建进程级单例校验器，opts 仅在首次
// 调用时生效.
func NewValidator(opts ...Option) *Validator {
	once.Do(func() {
		validate := validator.New(validator.WithRequiredStructEnabled())

		zhTrans := zh.New()
		trans, _ := ut.New(zhTrans, zhTrans).GetTranslator("zh")
		err := zhcn.RegisterDefaultTranslations(validate, trans)
		if err != nil {
			panic(err)
		}

		for _, f := range opts {
			f(validate, trans)
		}

		vd = &Validator{
			validate:   validate,
			translator: trans,
		}
	})

	return vd
}

// V is shorthand for NewValidator | V 是 NewValidator 简写.
func V(opts ...Option) *Validator {
	return NewValidator(opts...)
}

// ValidateStruct validates a struct | ValidateStruct 验证结构体.
func ValidateStruct(obj any) error {
	return vd.Validate(obj)
}

// ValidateStructContext validates a struct with a Context | ValidateStructContext
// 验证结构体，带 Context.
func ValidateStructContext(ctx context.Context, obj any) error {
	return vd.ValidateContext(ctx, obj)
}

// ValidatePartial validates only the fields named in partials | ValidatePartial
// 只验证 partials 列出的字段.
func ValidatePartial(obj any, partials map[string]struct{}) error {
	return vd.ValidatePartial(obj, partials)
}
